const FALLBACK_ICON_PATH = "/static/earth.svg";

// Use capture phase (true) because load/error events on images do not bubble
document.addEventListener("error", (event) => {
    const img = event.target;

    if (img.tagName !== "IMG") {
        return;
    }
    if (img.getAttribute("src") === FALLBACK_ICON_PATH) {
        return;
    }
    console.warn("Error loading image, applying fallback:", img.src);
    img.src = FALLBACK_ICON_PATH;
}, true);


// Dialogs
const addDialog = document.querySelector('#add-dialog');
const editDialog = document.querySelector('#edit-dialog');
const addShowButton = document.querySelector('#add-link');
const dialogCloseButtons = document.querySelectorAll('dialog .close-button');

// Open Add dialog
if (addShowButton && addDialog) {
    addShowButton.addEventListener("click", (e) => {
        e.preventDefault();
        e.stopPropagation();
        addDialog.showModal();
    });
}

// Close buttons for any dialog
if (dialogCloseButtons && dialogCloseButtons.length) {
    dialogCloseButtons.forEach(btn => {
        btn.addEventListener('click', (e) => {
            e.preventDefault();
            const dlg = btn.closest('dialog');
            if (dlg) dlg.close();
        });
    });
}

// Lightweight dropdown menu handling for each link item
(function setupItemMenus() {
    const containers = Array.from(document.querySelectorAll('.menu-container'));
    if (!containers.length) return;

    const closeAll = () => {
        containers.forEach(c => {
            c.classList.remove('open');
            const btn = c.querySelector('.item-menu-btn');
            if (btn) btn.setAttribute('aria-expanded', 'false');
        });
    };

    containers.forEach(container => {
        const btn = container.querySelector('.item-menu-btn');
        if (!btn) return;
        btn.addEventListener('click', (e) => {
            e.preventDefault();
            e.stopPropagation();
            const willOpen = !container.classList.contains('open');
            closeAll();
            if (willOpen) {
                container.classList.add('open');
                btn.setAttribute('aria-expanded', 'true');
            }
        });

        // Prevent clicks inside the menu from bubbling to card
        const menu = container.querySelector('.item-menu');
        if (menu) {
            menu.addEventListener('click', (e) => {
                e.stopPropagation();
            });

            // Handle Edit action to open edit dialog
            const editLinks = menu.querySelectorAll('.menu-item[data-action="edit"]');
            editLinks.forEach(link => {
                link.addEventListener('click', (e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    if (!editDialog) return;

                    // Fill the form fields from data- attributes
                    const id = link.getAttribute('data-id') || '';
                    const name = link.getAttribute('data-name') || '';
                    const url = link.getAttribute('data-url') || '';
                    const favicon = link.getAttribute('data-favicon') || '';

                    const idInput = document.querySelector('#edit-id');
                    const nameInput = document.querySelector('#edit-name');
                    const urlInput = document.querySelector('#edit-url');
                    const faviconInput = document.querySelector('#edit-favicon');

                    if (idInput) idInput.value = id;
                    if (nameInput) nameInput.value = name;
                    if (urlInput) urlInput.value = url;
                    if (faviconInput) faviconInput.value = favicon;

                    // Close menus
                    closeAll();

                    // Show dialog
                    editDialog.showModal();
                });
            });
        }
    });

    // Close on outside click
    document.addEventListener('click', (e) => {
        // If click is outside any menu container, close all
        if (!(e.target instanceof Element)) return;
        if (!e.target.closest('.menu-container')) {
            closeAll();
        }
    });

    // Close on Escape
    document.addEventListener('keydown', (e) => {
        if (e.key === 'Escape') closeAll();
    });
})();