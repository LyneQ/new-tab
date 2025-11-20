package main

import (
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
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

// Embed favicon, all files under static directory, and the default SQLite DB
//
//go:embed favicon.ico static/* identifier.sqlite
var embedded embed.FS
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

	/* =================================================================================================================
	 *                 HTTP section
	 * ===============================================================================================================*/

	// Serve embedded static files under /static/
	staticFS, err := fs.Sub(embedded, "static")
	if err != nil {
		fmt.Println("failed to prepare embedded static FS:", err)
		return
	}
	fileServer := http.FileServer(http.FS(staticFS))

	http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		data, _ := embedded.ReadFile("favicon.ico")
		w.Header().Set("Content-Type", "image/x-icon")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(data)
	})
	http.Handle("/static/", http.StripPrefix("/static/", fileServer))
	http.HandleFunc("/add", addRoute)
	http.HandleFunc("/edit", editRoute)
	http.HandleFunc("/delete", deleteRoute)
	http.HandleFunc("/move", moveRoute)
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
	//   <link rel="icon" href="/favicon.svg">
	//   <link href=/favicon.svg rel=icon>
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
	// Check if the database file exists.
	fi, statErr := os.Stat(databasePath)
	if statErr == nil && !fi.IsDir() {
		// Validate header of existing file.
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
	} else if os.IsNotExist(statErr) {
		// Try to seed the database from the embedded file if available.
		if data, err := embedded.ReadFile("identifier.sqlite"); err == nil {
			if writeErr := os.WriteFile(databasePath, data, 0o644); writeErr != nil {
				return nil, fmt.Errorf("[Error] failed to write embedded database file: %v", writeErr)
			}
		} else {
			// If the embedded DB is not present, we'll let sql.Open create a new file and initSchema will prepare it.
		}
	} else if statErr != nil {
		return nil, fmt.Errorf("[Error] failed to stat database file: %v", statErr)
	}

	// Open (and create if missing) the SQLite database using modernc.org/sqlite driver.
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, err
	}

	// Ensure required schema exists.
	if err := initSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func initSchema(db *sql.DB) error {
	_, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS links (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT,
            href TEXT NOT NULL,
            img TEXT,
            position INTEGER NOT NULL DEFAULT 0
        );
    `)
	return err
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

	// Render template from embedded filesystem
	tmpl := template.Must(template.New("search.html").Funcs(funcMap).ParseFS(embedded, "static/search.html"))

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
	if len(name) > 20 {
		name = name[:20]
	}

	_, err = db.Exec("INSERT INTO links (name, href, img, position) VALUES (?, ?, ?, ?)", name, href, img, position)
	if err != nil {
		http.Error(w, "Failed to insert link: "+err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, req, "/", http.StatusSeeOther)
}

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

func moveRoute(w http.ResponseWriter, req *http.Request) {
	// Accept GET only; other methods redirect back harmlessly
	if req.Method != http.MethodGet {
		http.Redirect(w, req, "/", http.StatusSeeOther)
		return
	}

	idStr := req.URL.Query().Get("id")
	dir := strings.ToLower(strings.TrimSpace(req.URL.Query().Get("dir")))
	if idStr == "" || (dir != "up" && dir != "down") {
		http.Redirect(w, req, "/", http.StatusSeeOther)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		http.Redirect(w, req, "/", http.StatusSeeOther)
		return
	}

	// Find the current link position
	var curPos int
	err = db.QueryRow("SELECT position FROM links WHERE id = ?", id).Scan(&curPos)
	if err != nil {
		// Link not found; just go back
		http.Redirect(w, req, "/", http.StatusSeeOther)
		return
	}

	// Find neighbor based on direction
	var neighborID, neighborPos int
	switch dir {
	case "up":
		err = db.QueryRow("SELECT id, position FROM links WHERE position < ? ORDER BY position DESC LIMIT 1", curPos).Scan(&neighborID, &neighborPos)
	case "down":
		err = db.QueryRow("SELECT id, position FROM links WHERE position > ? ORDER BY position ASC LIMIT 1", curPos).Scan(&neighborID, &neighborPos)
	}

	if err != nil {
		// No neighbor (already at boundary) or db error; go back silently
		http.Redirect(w, req, "/", http.StatusSeeOther)
		return
	}

	// Swap positions atomically
	tx, err := db.Begin()
	if err != nil {
		http.Redirect(w, req, "/", http.StatusSeeOther)
		return
	}
	// Temporary position to avoid unique constraints if any
	if _, err = tx.Exec("UPDATE links SET position = -1 WHERE id = ?", id); err != nil {
		_ = tx.Rollback()
		http.Redirect(w, req, "/", http.StatusSeeOther)
		return
	}
	if _, err = tx.Exec("UPDATE links SET position = ? WHERE id = ?", curPos, neighborID); err != nil {
		_ = tx.Rollback()
		http.Redirect(w, req, "/", http.StatusSeeOther)
		return
	}
	if _, err = tx.Exec("UPDATE links SET position = ? WHERE id = ?", neighborPos, id); err != nil {
		_ = tx.Rollback()
		http.Redirect(w, req, "/", http.StatusSeeOther)
		return
	}
	_ = tx.Commit()

	http.Redirect(w, req, "/", http.StatusSeeOther)
}
