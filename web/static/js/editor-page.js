// Editor page bootstrap. Extracted from an inline <script> in editor.html so the
// page can run under a strict Content-Security-Policy (script-src 'self').
//
// This MUST remain a classic, non-IIFE script: gopherwiki.js's gopherwiki_editor
// toolbar methods reference `cm_editor` as a free variable resolved through the
// shared global lexical environment of classic scripts. Wrapping this in a
// module or IIFE would hide cm_editor and break the toolbar.
//
// Per-page values (page path, base revision, initial cursor) are read from data
// attributes on #editor_block rather than interpolated into inline script.
const _editorConfig = document.getElementById("editor_block").dataset;
const pagepath = _editorConfig.pagepath;
const currentRevision = _editorConfig.revision;
const _cursorLine = parseInt(_editorConfig.cursorLine, 10) || 0;
const _cursorCh = parseInt(_editorConfig.cursorCh, 10) || 0;

// CSRF token for fetch-based mutations (draft save/delete, preview).
const _csrfMeta = document.querySelector('meta[name="csrf-token"]');
const csrfToken = _csrfMeta ? _csrfMeta.content : "";

let draftLoaded = false;
let lastSavedContent = "";
let autosaveTimer = null;

const editorParent = document.getElementById("editor_block");
const initialContent = document.getElementById("content_editor").value;
const cm_editor = GopherWikiEditor.createEditor(editorParent, initialContent);
cm_editor.setCursor({ line: _cursorLine, ch: _cursorCh });
cm_editor.focus();
lastSavedContent = cm_editor.getValue();

const preview_btn = document.querySelector("#preview_btn");
const editor_btn = document.querySelector("#editor_btn");
const editor_block = cm_editor.display.wrapper;
const preview_block = document.querySelector("#preview_block");

/* Draft functions */
function saveDraft() {
    const content = cm_editor.getValue();
    if (content === lastSavedContent) {
        return;
    }
    const cursor = cm_editor.getCursor();
    const formData = new FormData();
    formData.append("content", content);
    formData.append("cursor_line", cursor.line);
    formData.append("cursor_ch", cursor.ch);

    fetch("/" + pagepath + "/draft", {
        method: 'POST',
        headers: { "X-CSRF-Token": csrfToken },
        body: formData,
    })
    .then(response => response.json())
    .then(data => {
        if (data.success) {
            lastSavedContent = content;
            console.log("Draft saved");
        }
    })
    .catch(err => console.log("Draft save error:", err));
}

function deleteDraft() {
    fetch("/" + pagepath + "/draft", {
        method: 'DELETE',
        headers: { "X-CSRF-Token": csrfToken },
    }).catch(err => console.log("Draft delete error:", err));
}

function loadDraft() {
    fetch("/" + pagepath + "/draft")
    .then(response => response.json())
    .then(data => {
        if (data.found && data.content) {
            if (data.revision === currentRevision || currentRevision === "") {
                if (data.content !== cm_editor.getValue()) {
                    if (confirm("A draft was found. Do you want to restore it?")) {
                        cm_editor.setValue(data.content);
                        cm_editor.setCursor({ line: data.cursor_line, ch: data.cursor_ch });
                        lastSavedContent = data.content;
                    } else {
                        deleteDraft();
                    }
                }
            }
        }
        draftLoaded = true;
    })
    .catch(err => {
        console.log("Draft load error:", err);
        draftLoaded = true;
    });
}

// Load draft on page load
loadDraft();

// Autosave every 30 seconds
autosaveTimer = setInterval(saveDraft, 30000);

// Save draft on editor change (debounced)
let changeTimer = null;
cm_editor.on("change", function() {
    if (changeTimer) clearTimeout(changeTimer);
    changeTimer = setTimeout(saveDraft, 5000);
});

/* save */
document.getElementById('saveform').onsubmit = function() {
    const content_editor = cm_editor.getValue();
    document.getElementById('save_content').value = content_editor;
    document.getElementById('save_revision').value = currentRevision;
    deleteDraft();
    if (autosaveTimer) clearInterval(autosaveTimer);
};

/* preview */
preview_btn.onclick = function() {
    preview_block.style.display = 'block';
    editor_block.style.display = 'none';
    editor_btn.style.display = '';
    preview_btn.style.display = 'none';

    const formData = new FormData();
    formData.append("content", cm_editor.getValue());

    fetch("/" + pagepath + "/preview", {
            method: 'POST',
            headers: { "X-CSRF-Token": csrfToken },
            body: formData,
        })
        .then(function (response) {
            return response.json();
        })
        .then(function (data) {
            preview_block.innerHTML = data.preview_content;
        })
        .catch(function () {
            console.log('Error fetching preview ...');
        });
}

/* edit */
editor_btn.onclick = function() {
    preview_block.style.display = 'none';
    editor_block.style.display = 'block';
    editor_btn.style.display = 'none';
    preview_btn.style.display = '';
    cm_editor.focus();
}

/* Save draft before leaving page */
window.addEventListener('beforeunload', function(e) {
    if (cm_editor.getValue() !== lastSavedContent) {
        saveDraft();
    }
});
