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
const { blockOf, inline, renderMd, nodeToSrc, mdOf, MD_SPEC } = await load(inert,
    ['blockOf', 'inline', 'renderMd', 'nodeToSrc', 'mdOf', 'MD_SPEC']);

// blockOf: one raw line -> block element spec
let b = blockOf('# Hello');
assert.equal(b.tag, 'h1'); assert.equal(b.html, 'Hello');
b = blockOf('###### deep');
assert.equal(b.tag, 'h6');
b = blockOf('> quoted');
assert.equal(b.tag, 'blockquote'); assert.equal(b.html, 'quoted');
b = blockOf('- [ ] buy milk');
assert.equal(b.tag, 'task'); assert.equal(b.checked, false);
b = blockOf('- [x] done');
assert.equal(b.checked, true);
b = blockOf('- item');
assert.equal(b.tag, 'li');
b = blockOf('1. first');
assert.equal(b.tag, 'oli');
b = blockOf('---');
assert.equal(b.tag, 'hr');
b = blockOf('plain text');
assert.equal(b.tag, 'div'); assert.equal(b.html, 'plain text');

// inline markdown -> html
assert.equal(inline('**bold**'), '<strong>bold</strong>');
assert.equal(inline('_it_'), '<em>it</em>');
assert.equal(inline('~~gone~~'), '<s>gone</s>');
assert.equal(inline('`code`'), '<code>code</code>');
assert.equal(inline('[t](https://x)'), '<a href="https://x">t</a>');
assert.equal(inline('![a](i.png)'), '<img src="i.png" alt="a">');
assert.equal(inline('<b>esc</b>'), '&lt;b&gt;esc&lt;/b&gt;');

// renderMd: full source -> editor html, consecutive items group into a list
assert.equal(
    renderMd('# T\n\nhello\n\n- a\n- b'),
    '<h1>T</h1><div></div><div>hello</div><div></div><ul><li>a</li><li>b</li></ul>');
assert.equal(
    renderMd('- [ ] a\n- [x] b'),
    '<ul><li class="task"><input type="checkbox"> a</li><li class="task"><input type="checkbox" checked> b</li></ul>');
assert.equal(
    renderMd('1. one\n2. two'),
    '<ol><li>one</li><li>two</li></ol>');
assert.equal(renderMd('---'), '<hr>');
assert.equal(renderMd('```\n# not a heading\n```'),
    '<div>```</div><div># not a heading</div><div>```</div>');

// nodeToSrc: rendered tree -> raw markdown (round trip)
const el = (tagName, childNodes, extra = {}) => ({
    nodeType: 1, tagName, classList: { contains: () => false }, childNodes, getAttribute: extra.getAttribute, alt: extra.alt, textContent: extra.text,
});
const txt = (textContent) => ({ nodeType: 3, textContent });
const tree = el('H1', [txt('Hello '), el('STRONG', [txt('world')])]);
assert.equal(nodeToSrc(tree), '# Hello **world**');
assert.equal(nodeToSrc(el('CODE', [], { text: 'x' })), '`x`');
assert.equal(nodeToSrc(el('A', [txt('t')], { getAttribute: () => 'u' })), '[t](u)');
assert.equal(nodeToSrc(el('OL', [el('LI', [txt('one')]), el('LI', [txt('two')])])), '1. one\n2. two');
const taskLi = { nodeType: 1, tagName: 'LI', classList: { contains: (c) => c === 'task' }, childNodes: [{ nodeType: 1, tagName: 'INPUT', checked: true, classList: { contains: () => false }, childNodes: [] }, txt('done')] };
const ul = { nodeType: 1, tagName: 'UL', classList: { contains: () => false }, childNodes: [taskLi] };
assert.equal(nodeToSrc(ul), '- [x] done');
const editorTree = { childNodes: [el('H1', [txt('T')]), el('DIV', [txt('body')])] };
assert.equal(mdOf(editorTree), '# T\nbody');

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
