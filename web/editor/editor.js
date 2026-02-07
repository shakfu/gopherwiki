import { EditorState, Compartment } from "@codemirror/state";
import { EditorView, keymap, lineNumbers, highlightActiveLine, drawSelection } from "@codemirror/view";
import { defaultKeymap, history, historyKeymap, undo, redo, indentMore, indentLess } from "@codemirror/commands";
import { markdown, markdownLanguage } from "@codemirror/lang-markdown";
import { search, searchKeymap } from "@codemirror/search";
import { indentUnit } from "@codemirror/language";
import { lightTheme, darkTheme } from "./theme.js";
import { markdownListContinuation } from "./markdown-list.js";

/**
 * EditorAdapter -- wraps CM6 EditorView with a CM4-compatible API.
 *
 * gopherwiki.js calls cm_editor.getLine(), cm_editor.getCursor(), etc.
 * This adapter translates those calls to CM6 equivalents.
 *
 * CM4 uses 0-indexed lines. CM6 doc.line() uses 1-indexed.
 * All public methods here accept/return 0-indexed {line, ch} objects.
 */
class EditorAdapter {
    constructor(view) {
        this.view = view;
        this._changeHandlers = [];

        // CM4 compat: cm_editor.display.wrapper -> DOM element
        this.display = { wrapper: view.dom };

        // CM4 compat: cm_editor.doc -> proxy to this adapter
        this.doc = this;
    }

    // --- Value ---
    getValue() {
        return this.view.state.doc.toString();
    }

    setValue(text) {
        this.view.dispatch({
            changes: { from: 0, to: this.view.state.doc.length, insert: text },
        });
    }

    // --- Cursor ---
    getCursor(which) {
        const sel = this.view.state.selection.main;
        let offset;
        if (which === "start") {
            offset = Math.min(sel.anchor, sel.head);
        } else if (which === "end") {
            offset = Math.max(sel.anchor, sel.head);
        } else {
            offset = sel.head;
        }
        return this._offsetToPos(offset);
    }

    setCursor(pos) {
        const offset = this._posToOffset(pos);
        this.view.dispatch({ selection: { anchor: offset, head: offset } });
    }

    // --- Selection ---
    getSelection() {
        const sel = this.view.state.selection.main;
        return this.view.state.sliceDoc(sel.from, sel.to);
    }

    setSelection(anchor, head) {
        // CM4 setSelection(anchor, head) where anchor/head are {line, ch}
        const a = this._posToOffset(anchor);
        const h = this._posToOffset(head);
        this.view.dispatch({ selection: { anchor: a, head: h } });
    }

    replaceSelection(text, select) {
        const sel = this.view.state.selection.main;
        const from = sel.from;
        if (select === "around") {
            this.view.dispatch({
                changes: { from: sel.from, to: sel.to, insert: text },
                selection: { anchor: from, head: from + text.length },
            });
        } else {
            this.view.dispatch(
                this.view.state.replaceSelection(text)
            );
        }
    }

    somethingSelected() {
        const sel = this.view.state.selection.main;
        return sel.from !== sel.to;
    }

    // --- Lines ---
    getLine(n) {
        // CM4: 0-indexed. CM6 doc.line(): 1-indexed.
        const lineNum = n + 1;
        if (lineNum < 1 || lineNum > this.view.state.doc.lines) return "";
        return this.view.state.doc.line(lineNum).text;
    }

    lineCount() {
        return this.view.state.doc.lines;
    }

    lastLine() {
        return this.view.state.doc.lines - 1;
    }

    // --- Replace ---
    replaceRange(text, from, to) {
        const f = this._posToOffset(from);
        const t = to ? this._posToOffset(to) : f;
        this.view.dispatch({ changes: { from: f, to: t, insert: text } });
    }

    // --- Undo/Redo ---
    undo() { undo(this.view); }
    redo() { redo(this.view); }

    // --- Focus ---
    focus() { this.view.focus(); }

    // --- Events ---
    on(event, handler) {
        if (event === "change") {
            this._changeHandlers.push(handler);
        }
    }

    _notifyChange() {
        for (const handler of this._changeHandlers) {
            handler(this);
        }
    }

    // --- Token detection (replaces CM4 getTokenAt) ---
    getTokenAt(pos) {
        // Return a minimal token-like object by inspecting line content.
        // This is used by _getState() in gopherwiki.js.
        const line = this.getLine(pos.line);
        const ch = pos.ch;

        const result = { type: null, linkHref: false, linkText: false, linkTitle: false,
                         image: false, imageAltText: false, imageMarker: false, code: 0 };

        // Check if inside inline code
        let backtickCount = 0;
        let inCode = false;
        for (let i = 0; i < line.length; i++) {
            if (line[i] === '`') {
                backtickCount++;
            } else if (backtickCount > 0) {
                inCode = !inCode;
                backtickCount = 0;
            }
            if (i === ch - 1 && inCode) {
                result.code = 1;
                result.type = "comment";
                return result;
            }
        }

        // Build type string from markdown context
        const types = [];

        // Check for heading
        const headingMatch = line.match(/^(#{1,6})\s/);
        if (headingMatch) {
            types.push("header");
            types.push("header-" + headingMatch[1].length);
        }

        // Check for bold/italic/strikethrough at cursor position
        // Simple approach: scan for markers surrounding the cursor
        const before = line.slice(0, ch);
        const after = line.slice(ch);

        if ((before.match(/\*\*/g) || []).length % 2 === 1 && after.includes("**")) {
            types.push("strong");
        }
        if ((before.match(/__/g) || []).length % 2 === 1 && after.includes("__")) {
            types.push("strong");
        }
        // Single * or _ for italic (but not ** or __)
        const singleStarBefore = (before.match(/(?<!\*)\*(?!\*)/g) || []).length;
        const singleStarAfter = after.includes("*");
        if (singleStarBefore % 2 === 1 && singleStarAfter) {
            types.push("em");
        }
        const singleUnderBefore = (before.match(/(?<!_)_(?!_)/g) || []).length;
        const singleUnderAfter = after.includes("_");
        if (singleUnderBefore % 2 === 1 && singleUnderAfter) {
            types.push("em");
        }

        if ((before.match(/~~/g) || []).length % 2 === 1 && after.includes("~~")) {
            types.push("strikethrough");
        }

        // Check for blockquote
        if (/^\s*>/.test(line)) {
            types.push("quote");
        }

        // Check for list
        if (/^\s*\d+\.\s/.test(line)) {
            types.push("variable-2");
        } else if (/^\s*[-+*]\s/.test(line)) {
            types.push("variable-2");
        }

        // Check for link: [text](url)
        const linkRE = /\[([^\]]*)\]\(([^)]*)\)/g;
        let linkMatch;
        while ((linkMatch = linkRE.exec(line)) !== null) {
            const start = linkMatch.index;
            const end = start + linkMatch[0].length;
            if (ch >= start && ch <= end) {
                // Check if image: ![alt](url)
                if (start > 0 && line[start - 1] === '!') {
                    result.image = true;
                } else {
                    result.linkText = true;
                    types.push("link");
                }
            }
        }

        // Check for image: ![
        const imgRE = /!\[([^\]]*)\]\(([^)]*)\)/g;
        let imgMatch;
        while ((imgMatch = imgRE.exec(line)) !== null) {
            const start = imgMatch.index;
            const end = start + imgMatch[0].length;
            if (ch >= start && ch <= end) {
                result.image = true;
                result.imageAltText = true;
            }
        }

        result.type = types.length > 0 ? types.join(" ") : null;
        return result;
    }

    // --- Internal helpers ---
    _offsetToPos(offset) {
        const line = this.view.state.doc.lineAt(offset);
        return { line: line.number - 1, ch: offset - line.from };
    }

    _posToOffset(pos) {
        // Handle {line, ch} with 0-indexed lines
        const lineNum = Math.max(1, Math.min(pos.line + 1, this.view.state.doc.lines));
        const lineObj = this.view.state.doc.line(lineNum);
        const ch = Math.min(pos.ch, lineObj.length);
        return lineObj.from + ch;
    }
}

/**
 * Create a CM6 editor instance and return an EditorAdapter.
 */
function createEditor(parentElement, initialContent, options = {}) {
    const themeCompartment = new Compartment();
    const isDark = document.documentElement.getAttribute("data-theme") === "dark";

    const updateListener = EditorView.updateListener.of((update) => {
        if (update.docChanged && adapter) {
            adapter._notifyChange();
        }
    });

    const extensions = [
        lineNumbers(),
        highlightActiveLine(),
        drawSelection(),
        history(),
        EditorView.lineWrapping,
        EditorState.tabSize.of(4),
        indentUnit.of("    "),
        markdown({ base: markdownLanguage }),
        search(),
        keymap.of([
            { key: "Enter", run: markdownListContinuation },
            { key: "Tab", run: (view) => {
                if (view.state.selection.ranges.some(r => !r.empty)) {
                    indentMore(view);
                } else {
                    view.dispatch(view.state.update(
                        view.state.replaceSelection("    ")
                    ));
                }
                return true;
            }},
            { key: "Shift-Tab", run: indentLess },
            ...historyKeymap,
            ...defaultKeymap,
            ...searchKeymap,
        ]),
        themeCompartment.of(isDark ? darkTheme : darkTheme),
        updateListener,
    ];

    // Set initial theme correctly
    extensions[extensions.length - 2] = themeCompartment.of(isDark ? darkTheme : lightTheme);

    const state = EditorState.create({
        doc: initialContent,
        extensions,
    });

    const view = new EditorView({
        state,
        parent: parentElement,
    });

    const adapter = new EditorAdapter(view);

    // Observe theme changes on <html> to switch editor theme
    const observer = new MutationObserver(() => {
        const dark = document.documentElement.getAttribute("data-theme") === "dark";
        view.dispatch({
            effects: themeCompartment.reconfigure(dark ? darkTheme : lightTheme),
        });
    });
    observer.observe(document.documentElement, {
        attributes: true,
        attributeFilter: ["data-theme"],
    });

    // Store references for cleanup
    adapter._themeCompartment = themeCompartment;
    adapter._themeObserver = observer;

    return adapter;
}

// Export to global scope for use from editor.html
window.GopherWikiEditor = { createEditor, EditorAdapter };
