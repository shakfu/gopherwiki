import { EditorView } from "@codemirror/view";
import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { tags } from "@lezer/highlight";

/* Light theme -- based on idea.css / gopherwiki CM4 theme */
const lightEditorTheme = EditorView.theme({
    "&": {
        backgroundColor: "#ffffff",
        color: "#000000",
        fontFamily: "Consolas, Menlo, Monaco, 'Lucida Console', 'Liberation Mono', 'DejaVu Sans Mono', 'Bitstream Vera Sans Mono', 'Courier New', monospace, serif",
    },
    ".cm-content": { caretColor: "#000" },
    ".cm-cursor, .cm-dropCursor": { borderLeftColor: "#000" },
    "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection": {
        backgroundColor: "#d7d4f0",
    },
    ".cm-gutters": {
        backgroundColor: "#f5f5f5",
        color: "#999",
        borderRight: "1px solid #ddd",
    },
    ".cm-activeLineGutter": { backgroundColor: "#e8e8e8" },
    ".cm-activeLine": { backgroundColor: "#FFFAE3" },
    ".cm-matchingBracket": { outline: "1px solid grey", color: "black" },
}, { dark: false });

const lightSpecs = [
    { tag: tags.meta, color: "#808000" },
    { tag: tags.number, color: "#0000FF" },
    { tag: tags.heading, fontWeight: "bold", color: "#000080" },
    { tag: tags.keyword, fontWeight: "bold", color: "#000080" },
    { tag: tags.atom, fontWeight: "bold", color: "#000080" },
    { tag: tags.variableName, color: "#730" },
    { tag: tags.propertyName, color: "black" },
    { tag: tags.operator, color: "black" },
    { tag: tags.comment, color: "#808080" },
    { tag: tags.string, color: "#f99b15" },
    { tag: tags.invalid, color: "#FF0000" },
    { tag: tags.attributeName, color: "#0000FF" },
    { tag: tags.tagName, color: "#000080" },
    { tag: tags.quote, color: "#2b4" },
    { tag: tags.url, color: "#f99b15" },
    { tag: tags.link, color: "#1890ff", textDecoration: "none" },
    { tag: tags.strong, fontWeight: "bold" },
    { tag: tags.emphasis, fontStyle: "italic" },
    { tag: tags.strikethrough, textDecoration: "line-through" },
].filter(s => { if (!s.tag) { console.warn("gopherwiki: skipping undefined tag in light theme", s); return false; } return true; });
const lightHighlightStyle = HighlightStyle.define(lightSpecs);

/* Dark theme -- based on Nord */
const darkEditorTheme = EditorView.theme({
    "&": {
        backgroundColor: "#25282c",
        color: "#d8dee9",
    },
    ".cm-content": { caretColor: "#f8f8f0" },
    ".cm-cursor, .cm-dropCursor": { borderLeftColor: "#f8f8f0" },
    "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection": {
        backgroundColor: "#434c5e",
    },
    ".cm-gutters": {
        backgroundColor: "#25282c",
        color: "#4c566a",
        borderRight: "1px solid #4c566a",
    },
    ".cm-activeLineGutter": { backgroundColor: "#3b4252" },
    ".cm-activeLine": { backgroundColor: "#3b4252" },
    ".cm-matchingBracket": { textDecoration: "underline", color: "white !important" },
}, { dark: true });

const darkSpecs = [
    { tag: tags.comment, color: "#6BBAFF" },
    { tag: tags.atom, color: "#b48ead" },
    { tag: tags.number, color: "#b48ead" },
    { tag: tags.propertyName, color: "#8FBCBB" },
    { tag: tags.attributeName, color: "#8FBCBB" },
    { tag: tags.operator, color: "#81A1C1" },
    { tag: tags.keyword, color: "#81A1C1" },
    { tag: tags.string, color: "#A3BE8C" },
    { tag: tags.variableName, color: "#d8dee9" },
    { tag: tags.tagName, color: "#bf616a" },
    { tag: tags.heading, color: "#b48ead", fontWeight: "bold" },
    { tag: tags.url, color: "#A3BE8C" },
    { tag: tags.link, color: "#1890ff" },
    { tag: tags.invalid, backgroundColor: "#bf616a", color: "#f8f8f0" },
    { tag: tags.strong, fontWeight: "bold" },
    { tag: tags.emphasis, fontStyle: "italic" },
    { tag: tags.strikethrough, textDecoration: "line-through" },
].filter(s => { if (!s.tag) { console.warn("gopherwiki: skipping undefined tag in dark theme", s); return false; } return true; });
const darkHighlightStyle = HighlightStyle.define(darkSpecs);

export const lightTheme = [lightEditorTheme, syntaxHighlighting(lightHighlightStyle)];
export const darkTheme = [darkEditorTheme, syntaxHighlighting(darkHighlightStyle)];
