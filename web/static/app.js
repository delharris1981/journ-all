document.body.addEventListener('htmx:configRequest', function(evt) {
    evt.detail.headers['X-Requested-With'] = 'XMLHttpRequest';
});

document.body.addEventListener('htmx:responseError', function(evt) {
    console.error('htmx error', evt.detail);
});

// Markdown formatting toolbar. One spec drives both the buttons and the
// formatting, so adding a format is a single entry.
//   w: [open, close, placeholder]  wraps the selection (or inserts placeholder)
//   p: prefix                   applied to every selected line
//   t: template                 inserted as-is, caret lands after it
const MD_SPEC = [
    { k: 'h1', i: 'H1', t: 'Heading 1', p: '# ', ph: 'Heading 1' },
    { k: 'h2', i: 'H2', t: 'Heading 2', p: '## ', ph: 'Heading 2' },
    { k: 'h3', i: 'H3', t: 'Heading 3', p: '### ', ph: 'Heading 3' },
    { k: 'h4', i: 'H4', t: 'Heading 4', p: '#### ', ph: 'Heading 4' },
    { k: 'h5', i: 'H5', t: 'Heading 5', p: '##### ', ph: 'Heading 5' },
    { k: 'h6', i: 'H6', t: 'Heading 6', p: '###### ', ph: 'Heading 6' },
    { sep: true },
    { k: 'bold', i: '<b>B</b>', t: 'Bold', w: ['**', '**', 'bold text'] },
    { k: 'italic', i: '<i>I</i>', t: 'Italic', w: ['_', '_', 'italic text'] },
    { k: 'strike', i: '<s>S</s>', t: 'Strikethrough', w: ['~~', '~~', 'struck text'] },
    { k: 'code', i: '&lt;&gt;', t: 'Inline code', w: ['`', '`', 'code'] },
    { sep: true },
    { k: 'ul', i: '&bull;', t: 'Bullet list', p: '- ', ph: 'List item' },
    { k: 'ol', i: '1.', t: 'Numbered list', tpl: (l, i) => (i + 1) + '. ', ph: 'List item' },
    { k: 'task', i: '&#9744;', t: 'Task list', p: '- [ ] ', ph: 'Task' },
    { k: 'quote', i: '&rdquo;', t: 'Quote', p: '> ', ph: 'Quote' },
    { sep: true },
    { k: 'link', i: '&#128279;', t: 'Link', w: ['[', '](https://)', 'link text'] },
    { k: 'image', i: '&#128247;', t: 'Image', w: ['![', '](image.png)', 'alt text'] },
    {
        k: 'table', i: '&#9638;', t: 'Table',
        block: '| Column 1 | Column 2 |\n| --- | --- |\n| Cell | Cell |',
    },
    {
        k: 'codeblock', i: '{ }', t: 'Code block',
        block: '```\n\n```', at: 4,
    },
    { k: 'hr', i: '&mdash;', t: 'Divider', block: '\n\n---\n\n' },
    { k: 'break', i: '&#9166;', t: 'Line break', block: '  \n' },
];

function mdButton(spec) {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'tb';
    b.dataset.md = spec.k;
    b.title = spec.t;
    b.innerHTML = spec.i;
    return b;
}

function renderToolbar(toolbar) {
    for (const spec of MD_SPEC) {
        if (spec.sep) {
            const s = document.createElement('span');
            s.className = 'tb-sep';
            toolbar.appendChild(s);
        } else {
            toolbar.appendChild(mdButton(spec));
        }
    }
}

function insertAt(textarea, text, selStart, selEnd) {
    textarea.setRangeText(text, selStart, selEnd, 'end');
}

function applyFormat(textarea, kind) {
    const spec = MD_SPEC.find(s => s.k === kind);
    if (!spec) return;
    const { selectionStart: s, selectionEnd: e, value: v } = textarea;

    if (spec.w) {
        const [open, close, fallback] = spec.w;
        const sel = v.slice(s, e) || fallback;
        insertAt(textarea, open + sel + close, s, e);
        textarea.setSelectionRange(s + open.length, s + open.length + sel.length);
    } else if (spec.p || spec.tpl) {
        const sel = v.slice(s, e) || spec.ph;
        const atLineStart = s === 0 || v[s - 1] === '\n';
        const first = spec.tpl ? spec.tpl('', 0) : spec.p;
        const body = sel.split('\n')
            .map((line, i) => (spec.tpl ? spec.tpl(line, i) : spec.p) + line)
            .join('\n');
        insertAt(textarea, (atLineStart ? '' : '\n') + body, s, e);
        const start = s + (atLineStart ? 0 : 1) + first.length;
        textarea.setSelectionRange(start, start + sel.length);
    } else {
        insertAt(textarea, spec.block, s, e);
        const at = s + (spec.at === undefined ? spec.block.length : spec.at);
        textarea.setSelectionRange(at, at);
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

// DOMContentLoaded targets document, not body, so this listener must be on
// document or the toolbar never renders.
document.addEventListener('DOMContentLoaded', function() {
    for (const toolbar of document.querySelectorAll('.toolbar[data-for]')) renderToolbar(toolbar);
});
