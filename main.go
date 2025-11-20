package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Link struct {
	ID       int
	Name     *string
	Href     string
	Img      *string
	Position int
}

var HttpPort string = "8080"
var db *sql.DB

func main() {
	fmt.Println("Starting server")

	var err error
	db, err = initializeSqlite3("./identifier.sqlite")
	if err != nil {
		fmt.Println(err)
		return
	}

	checkDatabaseHealth(db, true)

	/* ===================================================
	 *                 HTTP section
	 * ===================================================
	 */

	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	http.HandleFunc("/add", addRoute)
	http.HandleFunc("/edit", editRoute)
	http.HandleFunc("/delete", deleteRoute)
	http.HandleFunc("/headers", headersRoute)
	http.HandleFunc("/", searchRoute)

	_ = http.ListenAndServe(":"+HttpPort, nil)
	fmt.Println("Server started on port" + HttpPort)

}

func GetDomainFromURL(urlStr string) (string, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", err
	}
	return parsedURL.Host, nil
}

func getFaviconURL(domain string) string {
	// First try to fetch the homepage and look for <link rel="...icon..." href="...">
	client := http.Client{Timeout: 1500 * time.Millisecond}
	schemes := []string{"https", "http"}

	// Precompile regexes
	//   <link rel="icon" href="/favicon.ico">
	//   <link href=/favicon.ico rel=icon>
	//   <link rel='shortcut icon' href='...'>
	//   <link rel=apple-touch-icon href=/icon.png>
	linkIconRe := regexp.MustCompile(`(?is)<link\b[^>]*\brel\s*=\s*(?:"[^"]*icon[^"]*"|'[^']*icon[^']*'|[^"'\s>]*icon[^"'\s>]*)[^>]*>`) // link with rel containing icon

	// Extract href value, supporting double-quoted, single-quoted, or unquoted forms.
	hrefRe := regexp.MustCompile(`(?is)\bhref\s*=\s*(?:"([^"]+)"|'([^']+)'|([^"'\s>]+))`)

	for _, scheme := range schemes {
		pageURL := scheme + "://" + domain + "/"
		req, _ := http.NewRequest("GET", pageURL, nil)
		req.Header.Set("User-Agent", "newtab/1.0 (+https://example)")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		resp, err := client.Do(req)
		if err == nil && resp != nil && resp.Body != nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024)) // read up to 512KB
			_ = resp.Body.Close()

			// Try to restrict to <head> for performance
			s := string(body)
			sl := strings.ToLower(s)
			if idx := strings.Index(sl, "</head>"); idx > 0 {
				s = s[:idx]
			}

			if matches := linkIconRe.FindAllString(s, -1); len(matches) > 0 {
				for _, m := range matches {
					if hrefMatch := hrefRe.FindStringSubmatch(m); len(hrefMatch) >= 4 {
						// pick first non-empty capture
						href := ""
						for i := 1; i <= 3; i++ {
							if hrefMatch[i] != "" {
								href = strings.TrimSpace(hrefMatch[i])
								break
							}
						}
						if href == "" {
							continue
						}
						// Resolve URL relative to pageURL
						// Handle protocol-relative URLs
						if strings.HasPrefix(href, "//") {
							return "https:" + href
						}
						u, err := url.Parse(href)
						if err == nil {
							if u.Scheme == "" {
								base, _ := url.Parse(pageURL)
								return base.ResolveReference(u).String()
							}
							return u.String()
						}
					}
				}
			}
		}
	}

	// Final fallback
	return "./static/earth.svg"
}

func initializeSqlite3(databasePath string) (*sql.DB, error) {
	file, err := os.Open(databasePath)
	if err != nil {
		return nil, fmt.Errorf("[Error] failed to open database file: %v", err)
	}
	defer file.Close()

	header := make([]byte, 16)
	if _, err := file.Read(header); err != nil {
		return nil, fmt.Errorf("[Error] failed to read SQLite header: %v", err)
	}

	if string(header[:15]) != "SQLite format 3" {
		return nil, fmt.Errorf("[Error] invalid SQLite database file")
	}

	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, err
	}

	return db, nil
}

func checkDatabaseHealth(db *sql.DB, shouldLogResult bool) bool {

	var result string
	row, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name='links';")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
		return false
	}
	defer row.Close()
	for row.Next() {
		err := row.Scan(&result)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
			return false
		}

	}

	if result != "links" {
		fmt.Println("[Error] Database is not initialized or corrupted.")
		os.Exit(1)
	}

	if shouldLogResult {
		fmt.Println("Result: ", result)
	}

	return true
}

/* =====================================================================================================================
 *                                              Routes
 * ===================================================================================================================*/

func headersRoute(w http.ResponseWriter, req *http.Request) {

	for name, headers := range req.Header {
		for _, h := range headers {
			_, _ = fmt.Fprintf(w, "%v: %v\n", name, h)
		}
	}
}

func searchRoute(w http.ResponseWriter, req *http.Request) {
	rows, err := db.Query("SELECT id, name, href, img, position FROM links ORDER BY position")
	if err != nil {
		http.Error(w, "Database query error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var links []Link
	for rows.Next() {
		var link Link
		if err := rows.Scan(&link.ID, &link.Name, &link.Href, &link.Img, &link.Position); err != nil {
			http.Error(w, "Error scanning database rows", http.StatusInternalServerError)
			return
		}
		links = append(links, link)
	}

	// Define the helper function
	funcMap := template.FuncMap{
		"slicestr": func(s string, start, end int) string {
			if start < 0 {
				start = 0
			}
			if end > len(s) {
				end = len(s)
			}
			if start > end {
				return ""
			}
			return s[start:end]
		},
		"GetDomainFromURL": func(url string) string {
			if domain, err := GetDomainFromURL(url); err == nil {
				return domain
			}
			return url
		},
	}

	tmpl := template.Must(template.New("search.html").Funcs(funcMap).ParseFiles("static/search.html"))

	data := struct {
		Links []Link
	}{
		Links: links,
	}

	tmpl.Execute(w, data)
}

func addRoute(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := req.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	name := req.FormValue("name")
	href := req.FormValue("url")
	favicon := req.FormValue("favicon")

	if name == "" || href == "" {
		http.Error(w, "Name and URL are required", http.StatusBadRequest)
		return
	}

	// Calculate the next position
	var maxPos sql.NullInt64
	err := db.QueryRow("SELECT MAX(position) FROM links").Scan(&maxPos)
	position := 0
	if err == nil && maxPos.Valid {
		position = int(maxPos.Int64) + 1
	}

	// Handle optional favicon
	var img *string
	if favicon != "" {
		img = &favicon
	} else {
		if domain, err := GetDomainFromURL(href); err == nil {
			if foundFavicon := getFaviconURL(domain); foundFavicon != "" {
				img = &foundFavicon
			}
		}
	}

	// Trim name to max 20 chars (safe)
	name = name[:20]

	_, err = db.Exec("INSERT INTO links (name, href, img, position) VALUES (?, ?, ?, ?)", name, href, img, position)
	if err != nil {
		http.Error(w, "Failed to insert link: "+err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, req, "/", http.StatusSeeOther)
}

// editRoute handles editing an existing link (alias: /editm)
func editRoute(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		// Fallback for accidental GET (e.g., clicking the menu link without JS)
		http.Redirect(w, req, "/", http.StatusSeeOther)
		return
	}

	if err := req.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	idStr := req.FormValue("id")
	if idStr == "" {
		http.Error(w, "Missing id", http.StatusBadRequest)
		return
	}
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}

	name := req.FormValue("name")
	href := req.FormValue("url")
	favicon := req.FormValue("favicon")

	if name == "" || href == "" {
		http.Error(w, "Name and URL are required", http.StatusBadRequest)
		return
	}

	// Trim name to max 20 chars (safe)
	if len(name) > 20 {
		name = name[:20]
	}

	// Handle optional favicon: use provided, otherwise try to discover
	var img *string
	if favicon != "" {
		img = &favicon
	} else {
		if domain, err := GetDomainFromURL(href); err == nil {
			if foundFavicon := getFaviconURL(domain); foundFavicon != "" {
				img = &foundFavicon
			}
		}
	}

	// Perform update
	if img != nil {
		_, err = db.Exec("UPDATE links SET name = ?, href = ?, img = ? WHERE id = ?", name, href, img, id)
	} else {
		// Set img to NULL explicitly
		_, err = db.Exec("UPDATE links SET name = ?, href = ?, img = NULL WHERE id = ?", name, href, id)
	}
	if err != nil {
		http.Error(w, "Failed to update link: "+err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, req, "/", http.StatusSeeOther)
}

// deleteRoute handles deleting a link by id. Supports GET (query) and POST (form)
func deleteRoute(w http.ResponseWriter, req *http.Request) {
	var idStr string
	switch req.Method {
	case http.MethodPost:
		if err := req.ParseForm(); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}
		idStr = req.FormValue("id")
	case http.MethodGet:
		idStr = req.URL.Query().Get("id")
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if idStr == "" {
		http.Error(w, "Missing id", http.StatusBadRequest)
		return
	}
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}

	_, err = db.Exec("DELETE FROM links WHERE id = ?", id)
	if err != nil {
		http.Error(w, "Failed to delete link: "+err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, req, "/", http.StatusSeeOther)
}
