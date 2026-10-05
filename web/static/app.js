document.body.addEventListener('htmx:configRequest', function(evt) {
    evt.detail.headers['X-Requested-With'] = 'XMLHttpRequest';
});

// Light/dark theme toggle, persisted across visits.
document.addEventListener('DOMContentLoaded', function() {
    const btn = document.getElementById('theme-toggle');
    if (!btn) return;
    const label = () => { btn.textContent = document.documentElement.dataset.theme === 'dark' ? 'Dark' : 'Light'; };
    label();
    btn.addEventListener('click', function() {
        const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
        document.documentElement.dataset.theme = next;
        localStorage.setItem('theme', next);
        label();
    });
});

document.body.addEventListener('htmx:responseError', function(evt) {
    console.error('htmx error', evt.detail);
});

// Markdown formatting toolbar. One spec drives both the buttons and the
// formatting, so adding a format is a single entry.
//   cmd:   [execCommand name, value]  runs document.execCommand
//   wrap:  [open, close]              wraps the selection with literal md
//   insert/block/html:                inserted as-is (caret after)
const MD_SPEC = [
    { k: 'h1', i: 'H1', t: 'Heading 1', cmd: ['formatBlock', 'h1'] },
    { k: 'h2', i: 'H2', t: 'Heading 2', cmd: ['formatBlock', 'h2'] },
    { k: 'h3', i: 'H3', t: 'Heading 3', cmd: ['formatBlock', 'h3'] },
    { k: 'h4', i: 'H4', t: 'Heading 4', cmd: ['formatBlock', 'h4'] },
    { k: 'h5', i: 'H5', t: 'Heading 5', cmd: ['formatBlock', 'h5'] },
    { k: 'h6', i: 'H6', t: 'Heading 6', cmd: ['formatBlock', 'h6'] },
    { sep: true },
    { k: 'bold', i: '<b>B</b>', t: 'Bold', cmd: ['bold'] },
    { k: 'italic', i: '<i>I</i>', t: 'Italic', cmd: ['italic'] },
    { k: 'strike', i: '<s>S</s>', t: 'Strikethrough', cmd: ['strikeThrough'] },
    { k: 'code', i: '&lt;&gt;', t: 'Inline code', wrap: '`' },
    { sep: true },
    { k: 'ul', i: '&bull;', t: 'Bullet list', cmd: ['insertUnorderedList'] },
    { k: 'ol', i: '1.', t: 'Numbered list', cmd: ['insertOrderedList'] },
    { k: 'task', i: '&#9744;', t: 'Task list', insert: '- [ ] ' },
    { k: 'quote', i: '&rdquo;', t: 'Quote', cmd: ['formatBlock', 'blockquote'] },
    { sep: true },
    { k: 'link', i: '&#128279;', t: 'Link', wrap: ['[', '](https://)'] },
    { k: 'image', i: '&#128247;', t: 'Image', wrap: ['![', '](image.png)'] },
    {
        k: 'table', i: '&#9638;', t: 'Table',
        block: '| Column 1 | Column 2 |\n| --- | --- |\n| Cell | Cell |',
    },
    {
        k: 'codeblock', i: '{ }', t: 'Code block',
        block: '```\n\n```',
    },
    { k: 'hr', i: '&mdash;', t: 'Divider', html: '<hr>' },
    { k: 'break', i: '&#9166;', t: 'Line break', html: '  <br>' },
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

function selectionText() {
    const s = window.getSelection();
    return s && s.rangeCount ? s.getRangeAt(0).toString() : '';
}

function applyFormat(editor, kind) {
    const spec = MD_SPEC.find(s => s.k === kind);
    if (!spec) return;
    editor.focus();
    if (spec.cmd) {
        document.execCommand(spec.cmd[0], false, spec.cmd[1] || null);
    } else if (spec.wrap) {
        const [open, close] = Array.isArray(spec.wrap) ? spec.wrap : [spec.wrap, spec.wrap];
        document.execCommand('insertText', false, open + (selectionText() || 'text') + close);
    } else if (spec.insert) {
        document.execCommand('insertText', false, spec.insert);
    } else if (spec.block) {
        const html = spec.block.split('\n').map(esc).map(l => `<div>${l}</div>`).join('');
        document.execCommand('insertHTML', false, html);
    } else if (spec.html) {
        document.execCommand('insertHTML', false, spec.html);
    }
    editor.dispatchEvent(new Event('input', { bubbles: true }));
}

document.body.addEventListener('click', function(evt) {
    const btn = evt.target.closest('.tb[data-md]');
    if (!btn) return;
    evt.preventDefault();
    const editor = document.getElementById(btn.parentElement.dataset.for);
    if (editor) applyFormat(editor, btn.dataset.md);
});

// DOMContentLoaded targets document, not body, so this listener must be on
// document or the toolbar never renders.
document.addEventListener('DOMContentLoaded', function() {
    for (const toolbar of document.querySelectorAll('.toolbar[data-for]')) renderToolbar(toolbar);
    const editor = document.getElementById('editor');
    const src = document.getElementById('body-src');
    if (editor && src) initEditor(editor, src);
});

// ---------------------------------------------------------------------------
// Live WYSIWYG markdown editor. The visible surface shows rendered markdown;
// typing '# Title' turns that line into a heading in place. The hidden
// textarea (#body-src) always holds the raw markdown source that gets posted.
// ---------------------------------------------------------------------------

function esc(s) {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

// Inline markdown -> HTML. Order matters: code first so its contents are not
// re-interpreted, then the longer tokens before their shorter overlaps.
function inline(s) {
    let h = esc(s);
    h = h.replace(/`([^`]+)`/g, '<code>$1</code>');
    h = h.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    h = h.replace(/~~([^~]+)~~/g, '<s>$1</s>');
    h = h.replace(/_([^_]+)_/g, '<em>$1</em>');
    h = h.replace(/\*([^*\n]+)\*/g, '<em>$1</em>');
    h = h.replace(/!\[([^\]]*)\]\(([^)]+)\)/g, '<img src="$2" alt="$1">');
    h = h.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2">$1</a>');
    return h;
}

// One raw line -> the block element kind wrapping its rendered HTML.
function blockOf(line) {
    let m;
    if ((m = /^(#{1,6})\s+(.*)$/.exec(line))) return { tag: 'h' + m[1].length, html: inline(m[2]) };
    if (/^\s*(-{3,}|\*{3,}|_{3,})\s*$/.test(line)) return { tag: 'hr' };
    if ((m = /^>\s?(.*)$/.exec(line))) return { tag: 'blockquote', html: inline(m[1]) };
    if ((m = /^- \[([ xX])\]\s?(.*)$/.exec(line))) return { tag: 'task', checked: m[1] !== ' ', html: inline(m[2]) };
    if ((m = /^[-*]\s+(.*)$/.exec(line))) return { tag: 'li', html: inline(m[1]) };
    if ((m = /^(\d+)[.)]\s+(.*)$/.exec(line))) return { tag: 'oli', html: inline(m[2]) };
    return { tag: 'div', html: inline(line) };
}

// Markdown source -> editor HTML. Consecutive list lines group into one list.
function renderMd(src) {
    const out = [];
    let inCode = false, list = [], listTag = null;
    const flush = () => {
        if (listTag) {
            out.push(listTag === 'ul' ? `<ul>${list.join('')}</ul>` : `<ol>${list.join('')}</ol>`);
            list = []; listTag = null;
        }
    };
    for (const line of src.split('\n')) {
        if (line.trim() === '```') {
            flush();
            inCode = !inCode;
            out.push(`<div>${esc(line)}</div>`);
            continue;
        }
        if (inCode) { out.push(`<div>${esc(line)}</div>`); continue; }
        const b = blockOf(line);
        if (b.tag === 'li' || b.tag === 'oli' || b.tag === 'task') {
            const want = b.tag === 'oli' ? 'ol' : 'ul';
            if (listTag !== want) { flush(); listTag = want; }
            list.push(b.tag === 'task'
                ? `<li class="task"><input type="checkbox"${b.checked ? ' checked' : ''}> ${b.html}</li>`
                : `<li>${b.html}</li>`);
            continue;
        }
        flush();
        switch (b.tag) {
            case 'hr': out.push('<hr>'); break;
            case 'blockquote': out.push(`<blockquote>${b.html}</blockquote>`); break;
            default:
                out.push(b.tag === 'div' ? `<div>${b.html}</div>` : `<${b.tag}>${b.html}</${b.tag}>`);
        }
    }
    flush();
    return out.join('');
}

// DOM (or an equivalent tree: {nodeType:3, textContent} / {nodeType:1,
// tagName, classList, childNodes, getAttribute, alt}) -> raw markdown source.
function nodeToSrc(node) {
    if (node.nodeType === 3) return node.textContent;
    const el = node;
    const kids = () => [...el.childNodes].map(nodeToSrc).join('');
    const has = (c) => el.classList && el.classList.contains(c);
    switch (el.tagName) {
        case 'H1': case 'H2': case 'H3': case 'H4': case 'H5': case 'H6':
            return '#'.repeat(+el.tagName[1]) + ' ' + kids();
        case 'BLOCKQUOTE': return '> ' + kids();
        case 'UL': return [...el.childNodes].map(li => (has('task') || li.classList?.contains('task') ? nodeToSrc(li) : '- ' + [...li.childNodes].map(nodeToSrc).join(''))).join('\n');
        case 'OL': return [...el.childNodes].map((li, i) => (i + 1) + '. ' + [...li.childNodes].map(nodeToSrc).join('')).join('\n');
        case 'LI':
            if (el.classList && el.classList.contains('task')) {
                const box = [...el.childNodes].find(n => n.tagName === 'INPUT');
                return '- [' + (box && box.checked ? 'x' : ' ') + '] ' +
                    [...el.childNodes].filter(n => n.tagName !== 'INPUT').map(nodeToSrc).join('').replace(/^ /, '');
            }
            return kids();
        case 'HR': return '---';
        case 'PRE':
            return '```\n' + el.textContent + '\n```';
        case 'STRONG': case 'B': return '**' + kids() + '**';
        case 'EM': case 'I': return '_' + kids() + '_';
        case 'S': case 'STRIKE': case 'DEL': return '~~' + kids() + '~~';
        case 'CODE': return '`' + el.textContent + '`';
        case 'A': return '[' + kids() + '](' + el.getAttribute('href') + ')';
        case 'IMG': return '![' + (el.alt || '') + '](' + el.getAttribute('src') + ')';
        case 'BR': return '';
        default: return kids();
    }
}

function mdOf(editor) {
    return [...editor.childNodes].map(nodeToSrc).join('\n');
}

// --- live conversion -------------------------------------------------------

function caretBlock(editor) {
    let n = window.getSelection().anchorNode;
    while (n && n !== editor) {
        if (n.nodeType === 1 && /^(H[1-6]|BLOCKQUOTE|LI|DIV|P|PRE)$/.test(n.tagName)) return n;
        n = n.parentNode;
    }
    return null;
}

// Odd number of ``` before the caret means the caret is inside a code block.
function inCodeFence(editor) {
    const sel = window.getSelection();
    if (!sel.rangeCount) return false;
    const r = document.createRange();
    r.selectNodeContents(editor);
    r.setEnd(sel.anchorNode, sel.anchorOffset);
    return (r.toString().match(/```/g) || []).length % 2 === 1;
}

function placeCaret(el) {
    const r = document.createRange();
    r.selectNodeContents(el);
    r.collapse(false);
    const s = window.getSelection();
    s.removeAllRanges();
    s.addRange(r);
}

function replaceWith(block, html) {
    const t = document.createElement('template');
    t.innerHTML = html;
    const el = t.content.firstElementChild;
    block.replaceWith(el);
    placeCaret(el);
    return el;
}

const INLINE_RES = [
    [/!\[([^\]]*)\]\(([^)]+)\)/, m => ({ tag: 'img', attrs: { src: m[2], alt: m[1] } })],
    [/\[([^\]]+)\]\(([^)]+)\)/, m => ({ tag: 'a', attrs: { href: m[2] }, text: m[1] })],
    [/`([^`]+)`/, m => ({ tag: 'code', text: m[1] })],
    [/\*\*([^*]+)\*\*/, m => ({ tag: 'strong', text: m[1] })],
    [/~~([^~]+)~~/, m => ({ tag: 's', text: m[1] })],
    [/_([^_]+)_/, m => ({ tag: 'em', text: m[1] })],
    [/\*([^*\n]+)\*/, m => ({ tag: 'em', text: m[1] })],
];

// Convert the first completed inline pattern (**bold**, `code`, ...) inside
// the block into its rendered element. Returns true when something changed.
function convertInline(block) {
    for (const node of textNodes(block)) {
        if (node.parentElement && /^(CODE|PRE)$/.test(node.parentElement.tagName)) continue;
        for (const [re, make] of INLINE_RES) {
            const m = re.exec(node.textContent);
            if (!m) continue;
            const before = node.splitText(m.index);
            const hit = before.splitText(m[0].length);
            const spec = make(m);
            const el = document.createElement(spec.tag);
            if (spec.attrs) for (const [k, v] of Object.entries(spec.attrs)) el.setAttribute(k, v);
            if (spec.tag === 'img') { el.alt = spec.attrs.alt; } else { el.textContent = spec.text; }
            before.replaceWith(el);
            placeCaretAfter(el);
            return true;
        }
    }
    return false;
}

function textNodes(root) {
    const out = [];
    const walk = (n) => {
        for (const c of n.childNodes) {
            if (c.nodeType === 3) out.push(c);
            else if (c.nodeType === 1) walk(c);
        }
    };
    walk(root);
    return out;
}

function placeCaretAfter(el) {
    const r = document.createRange();
    r.setStartAfter(el);
    r.collapse(true);
    const s = window.getSelection();
    s.removeAllRanges();
    s.addRange(r);
}

function convertBlock(block, editor) {
    if (!block || !/^(DIV|P)$/.test(block.tagName)) return;
    const raw = [...block.childNodes].map(nodeToSrc).join('');
    // A block reverted to raw markdown via double-click keeps its raw form
    // only until its text actually changes, then it re-renders.
    if (block.dataset.raw) {
        if (raw === block.dataset.raw) return;
        delete block.dataset.raw;
    }
    let m;
    if ((m = /^(#{1,6})\s+(.*)$/.exec(raw))) {
        replaceWith(block, `<h${m[1].length}>${inline(m[2])}</h${m[1].length}>`);
    } else if ((m = /^>\s?(.*)$/.exec(raw))) {
        replaceWith(block, `<blockquote>${inline(m[1])}</blockquote>`);
    } else if ((m = /^- \[([ xX])\]\s?(.*)$/.exec(raw))) {
        wrapInList(block, editor, `<li class="task"><input type="checkbox"${m[1] !== ' ' ? ' checked' : ''}> ${inline(m[2])}</li>`, 'ul');
    } else if ((m = /^[-*]\s+(.*)$/.exec(raw))) {
        wrapInList(block, editor, `<li>${inline(m[1])}</li>`, 'ul');
    } else if ((m = /^\d+[.)]\s+(.*)$/.exec(raw))) {
        wrapInList(block, editor, `<li>${inline(m[1])}</li>`, 'ol');
    } else if (/^\s*(-{3,}|\*{3,}|_{3,})\s*$/.test(raw)) {
        const t = document.createElement('template');
        t.innerHTML = '<hr><div><br></div>';
        const div = t.content.querySelector('div');
        block.replaceWith(t.content);
        placeCaret(div);
    } else if (raw.trim() === '```') {
        // Either the opening fence (search forward) or the closing one
        // (search back): once both fences exist, wrap everything between
        // (inclusive) into one rendered code block.
        let open = null, close = null;
        let el = block.previousElementSibling;
        while (el) {
            if (el.tagName === 'DIV' && nodeToSrc(el).trim() === '```') { open = el; break; }
            el = el.previousElementSibling;
        }
        el = block.nextElementSibling;
        while (el) {
            if (el.tagName === 'DIV' && nodeToSrc(el).trim() === '```') { close = el; break; }
            el = el.nextElementSibling;
        }
        if (!open && close) { open = block; }
        if (!close && open) { close = block; }
        if (!open || !close || open === close) return;
        return wrapFencedRange(editor, open, close);
    }
}

function wrapFencedRange(editor, open, close) {
    const inner = [];
    let el = open.nextElementSibling;
    while (el && el !== close) { inner.push(el); el = el.nextElementSibling; }
    const code = inner.map(e => e.textContent).join('\n');
    const t = document.createElement('template');
    t.innerHTML = '<pre><code></code></pre><div><br></div>';
    t.content.querySelector('code').textContent = code;
    const pre = t.content.querySelector('pre');
    const after = t.content.querySelector('div');
    for (const e of inner) e.remove();
    close.remove();
    open.replaceWith(pre, after);
    placeCaret(after);
}

function wrapInList(block, editor, liHtml, tag) {
    const prev = block.previousElementSibling;
    const t = document.createElement('template');
    t.innerHTML = liHtml;
    const li = t.content.firstElementChild;
    if (prev && prev.tagName === tag.toUpperCase()) {
        prev.appendChild(li);
        block.remove();
    } else {
        const list = document.createElement(tag);
        list.appendChild(li);
        block.replaceWith(list);
    }
    placeCaret(li);
}

function initEditor(editor, src) {
    editor.innerHTML = renderMd(src.value) || '<div><br></div>';
    const sync = () => { src.value = mdOf(editor); };
    editor.addEventListener('input', () => {
        editor.classList.toggle('empty', editor.textContent === '');
        if (inCodeFence(editor)) { sync(); return; }
        const block = caretBlock(editor);
        if (block) convertBlock(block, editor);
        const after = caretBlock(editor);
        if (after) convertInline(after);
        sync();
    });
    editor.addEventListener('change', (e) => {
        if (e.target.type === 'checkbox') sync();
    });
    // Double-click a rendered block to edit its raw markdown again. The raw
    // flag suppresses instant re-rendering until the text actually changes.
    editor.addEventListener('dblclick', (e) => {
        const el = e.target.closest('h1,h2,h3,h4,h5,h6,blockquote,ul,ol,hr,pre');
        if (!el || el === editor) return;
        e.preventDefault();
        const div = document.createElement('div');
        div.textContent = nodeToSrc(el);
        div.dataset.raw = nodeToSrc(el);
        el.replaceWith(div);
        placeCaret(div);
    });
    editor.addEventListener('blur', sync);
    const form = editor.closest('form');
    if (form) form.addEventListener('submit', sync);
    sync();
}
