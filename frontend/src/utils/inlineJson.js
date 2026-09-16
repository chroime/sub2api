export function serializeInlineJson(value) {
    // JSON is embedded in a script element; Markdown may contain a closing script tag.
    return JSON.stringify(value).replace(/</g, '\\u003c');
}
