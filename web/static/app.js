document.body.addEventListener('htmx:configRequest', function(evt) {
    evt.detail.headers['X-Requested-With'] = 'XMLHttpRequest';
});

document.body.addEventListener('htmx:responseError', function(evt) {
    console.error('htmx error', evt.detail);
});

// Markdown formatting toolbar. Buttons wrap the selection, or insert a
// placeholder when nothing is selected.
const MD_WRAP = {
    bold: ['**', '**', 'bold text'],
    italic: ['_', '_', 'italic text'],
    code: ['`', '`', 'code'],
    link: ['[', '](https://)', 'link text'],
};
const MD_LINE = {
    h1: ['# ', 'Heading 1'],
    h2: ['## ', 'Heading 2'],
    h3: ['### ', 'Heading 3'],
};

function insertAt(textarea, text, selStart, selEnd) {
    textarea.setRangeText(text, selStart, selEnd, 'end');
}

function applyFormat(textarea, kind) {
    const { selectionStart: s, selectionEnd: e, value: v } = textarea;
    if (MD_WRAP[kind]) {
        const [open, close, fallback] = MD_WRAP[kind];
        const sel = v.slice(s, e) || fallback;
        insertAt(textarea, open + sel + close, s, e);
        textarea.setSelectionRange(s + open.length, s + open.length + sel.length);
    } else if (MD_LINE[kind]) {
        const [prefix, fallback] = MD_LINE[kind];
        const sel = v.slice(s, e) || fallback;
        const atLineStart = s === 0 || v[s - 1] === '\n';
        const text = (atLineStart ? '' : '\n') + prefix + sel.replace(/\n/g, '\n' + prefix);
        insertAt(textarea, text, s, e);
        textarea.setSelectionRange(s + prefix.length + 1, s + prefix.length + 1 + sel.length);
    } else if (kind === 'ul' || kind === 'ol') {
        const sel = v.slice(s, e) || 'List item';
        const text = sel.split('\n').map((line, i) => (kind === 'ul' ? '- ' : i + 1 + '. ') + line).join('\n');
        insertAt(textarea, text, s, e);
        textarea.setSelectionRange(s, s + text.length);
    } else if (kind === 'quote') {
        const sel = v.slice(s, e) || 'Quote';
        const text = sel.split('\n').map(line => '> ' + line).join('\n');
        insertAt(textarea, text, s, e);
        textarea.setSelectionRange(s, s + text.length);
    } else if (kind === 'hr') {
        insertAt(textarea, '\n\n---\n\n', s, e);
    }
    textarea.dispatchEvent(new Event('input', { bubbles: true }));
    textarea.focus();
}

document.body.addEventListener('click', function(evt) {
    const btn = evt.target.closest('.tb[data-md]');
    if (!btn) return;
    evt.preventDefault();
    const textarea = document.getElementById(btn.parentElement.dataset.for);
    if (textarea) applyFormat(textarea, btn.dataset.md);
});
