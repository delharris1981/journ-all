// Run: node web/app.test.mjs
import assert from 'node:assert';
import { readFileSync } from 'node:fs';

const src = readFileSync(new URL('./static/app.js', import.meta.url), 'utf8');
const load = async (stubs, names) => {
    const saved = { document: globalThis.document };
    globalThis.document = stubs;
    try {
        const mod = await import('data:text/javascript,' +
            encodeURIComponent(src + '\nexport { ' + names.join(', ') + ' };'));
        return mod;
    } finally {
        globalThis.document = saved.document;
    }
};

const inert = { body: { addEventListener() {} }, addEventListener() {} };
const { applyFormat, MD_SPEC } = await load(inert, ['applyFormat', 'MD_SPEC']);

// Fake textarea: setRangeText + selection only.
const ta = (value, start, end) => ({
    value, selectionStart: start, selectionEnd: end,
    focus() {},
    dispatchEvent() {},
    setRangeText(text, s, e) {
        this.value = this.value.slice(0, s) + text + this.value.slice(e);
    },
    setSelectionRange(s, e) { this.selectionStart = s; this.selectionEnd = e; },
});

const run = (kind, value = '', start = 0, end = start) => {
    const t = ta(value, start, end);
    applyFormat(t, kind);
    return t;
};

let t = run('bold');
assert.equal(t.value, '**bold text**');
assert.equal(t.selectionStart, 2, 'caret inside bold wrapper');

t = run('h1', 'hello world', 6, 11);
assert.equal(t.value, 'hello \n# world', 'heading prefix on its own line');
assert.equal(t.selectionStart, 9, 'caret after "# "');

t = run('ol', 'a\nb\nc', 0, 5);
assert.equal(t.value, '1. a\n2. b\n3. c', 'numbered list renumbers each line');

t = run('strike', 'gone', 0, 4);
assert.equal(t.value, '~~gone~~');

t = run('h6');
assert.equal(t.value, '###### Heading 6');

t = run('task', 'buy milk', 0, 8);
assert.equal(t.value, '- [ ] buy milk');

t = run('quote', 'one\ntwo', 0, 7);
assert.equal(t.value, '> one\n> two');

t = run('ul', 'x\ny', 0, 3);
assert.equal(t.value, '- x\n- y');

t = run('image', 'cat.png', 0, 7);
assert.equal(t.value, '![cat.png](image.png)');

t = run('link', 'site', 0, 4);
assert.equal(t.value, '[site](https://)');

t = run('code');
assert.equal(t.value, '`code`');

t = run('table');
assert.match(t.value, /^\| Column 1 \| Column 2 \|\n\| --- \| --- \|\n\| Cell \| Cell \|$/);

t = run('codeblock');
assert.equal(t.value, '```\n\n```');
assert.equal(t.selectionStart, 4, 'caret between the fences');

t = run('hr');
assert.equal(t.value, '\n\n---\n\n');

t = run('break');
assert.equal(t.value, '  \n');

assert.ok(MD_SPEC.filter(s => !s.sep).length >= 18, 'toolbar covers the md format set');

// The toolbar is built on DOMContentLoaded, which fires on document - a listener
// on document.body silently never runs, leaving an empty toolbar.
{
    const handlers = {};
    const toolbar = { children: [], appendChild(el) { this.children.push(el); } };
    const stubs = {
        body: { addEventListener() {} },
        addEventListener(type, fn) { handlers[type] = fn; },
        createElement: () => ({ dataset: {}, className: '' }),
        querySelectorAll: () => [toolbar],
        getElementById: () => null,
    };
    await load(stubs, ['MD_SPEC']);
    assert.ok(handlers.DOMContentLoaded, 'DOMContentLoaded listener must be on document');
    globalThis.document = stubs;
    handlers.DOMContentLoaded();
    assert.equal(toolbar.children.filter(el => el.className === 'tb').length,
        MD_SPEC.filter(s => !s.sep).length, 'every format gets a button');
    assert.equal(toolbar.children.filter(el => el.className === 'tb-sep').length,
        MD_SPEC.filter(s => s.sep).length, 'separators rendered');
}

console.log('ok');
