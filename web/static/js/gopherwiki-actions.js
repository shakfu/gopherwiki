// Delegated event handlers that replace inline on* attributes in the templates.
// Keeping these out of HTML attributes lets the app run under a strict
// Content-Security-Policy (script-src 'self', no 'unsafe-inline').
//
// Markup contract:
//   [data-action="toggle-sidebar"]    -> window.toggleSidebar()
//   [data-action="toggle-dark-mode"]  -> window.toggleDarkMode()
//   [data-action="toggle-modal"]      -> window.gopherwiki.toggleModal(data-target)
//   [data-editor-action="<method>"]   -> window.gopherwiki_editor.<method>()
//   form[data-confirm="<message>"]    -> confirm(message) before submit
(function () {
    "use strict";

    document.addEventListener("click", function (event) {
        var trigger = event.target.closest("[data-action]");
        if (trigger) {
            switch (trigger.getAttribute("data-action")) {
                case "toggle-sidebar":
                    if (typeof window.toggleSidebar === "function") {
                        window.toggleSidebar();
                    }
                    break;
                case "toggle-dark-mode":
                    event.preventDefault();
                    if (typeof window.toggleDarkMode === "function") {
                        window.toggleDarkMode();
                    }
                    break;
                case "toggle-modal":
                    if (window.gopherwiki && typeof window.gopherwiki.toggleModal === "function") {
                        window.gopherwiki.toggleModal(trigger.getAttribute("data-target"));
                    }
                    if (trigger.tagName === "A") {
                        event.preventDefault();
                    }
                    break;
            }
            return;
        }

        var editorBtn = event.target.closest("[data-editor-action]");
        if (editorBtn) {
            var method = editorBtn.getAttribute("data-editor-action");
            if (window.gopherwiki_editor && typeof window.gopherwiki_editor[method] === "function") {
                window.gopherwiki_editor[method]();
            }
        }
    });

    document.addEventListener("submit", function (event) {
        var form = event.target.closest("form[data-confirm]");
        if (form && !window.confirm(form.getAttribute("data-confirm"))) {
            event.preventDefault();
        }
    });
})();
