/* vim: set et sts=4 ts=4 sw=4 ai: */
/* GopherWiki UI - dark mode + sidebar toggle (replaces halfmoon.js) */

(function () {
    "use strict";

    var THEME_KEY = "gopherwiki-theme";
    var SIDEBAR_KEY = "gopherwiki-sidebar";

    function applyTheme(theme) {
        document.documentElement.setAttribute("data-theme", theme);
    }

    function getStored(key) {
        try { return localStorage.getItem(key); } catch (_) { return null; }
    }

    function store(key, value) {
        try { localStorage.setItem(key, value); } catch (_) { /* noop */ }
    }

    // Restore theme on load (runs synchronously in <head> or before paint)
    var stored = getStored(THEME_KEY);
    if (stored === "dark" || stored === "light") {
        applyTheme(stored);
    } else if (window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches) {
        applyTheme("dark");
    } else {
        applyTheme("light");
    }

    window.toggleDarkMode = function () {
        var current = document.documentElement.getAttribute("data-theme");
        var next = current === "dark" ? "light" : "dark";
        applyTheme(next);
        store(THEME_KEY, next);
    };

    // Restore sidebar state on DOM ready
    document.addEventListener("DOMContentLoaded", function () {
        if (getStored(SIDEBAR_KEY) === "collapsed") {
            var sidebar = document.querySelector(".wiki-sidebar");
            if (sidebar && window.innerWidth > 768) {
                sidebar.classList.add("collapsed");
            }
        }
    });

    window.toggleSidebar = function () {
        var sidebar = document.querySelector(".wiki-sidebar");
        var overlay = document.querySelector(".sidebar-overlay");
        if (!sidebar) return;

        if (window.innerWidth <= 768) {
            // Mobile: slide in/out
            sidebar.classList.toggle("open");
            if (overlay) overlay.classList.toggle("open");
        } else {
            // Desktop: collapse/expand
            sidebar.classList.toggle("collapsed");
            store(SIDEBAR_KEY, sidebar.classList.contains("collapsed") ? "collapsed" : "open");
        }
    };
})();
