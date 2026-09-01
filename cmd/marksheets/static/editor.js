// Marksheets editor.
//
// The document is a tree, but the editor works on a flat list of rows carrying
// a depth. Flat is what keyboard editing wants — indent, outdent and reorder
// are slices and arithmetic — and the tree is rebuilt only when saving.
(function () {
	'use strict';

	const shell = document.querySelector('.editor-shell');
	if (!shell) return;

	const slug = shell.dataset.slug;
	const rowsEl = document.getElementById('rows');
	const titleEl = document.querySelector('.doc-title');
	const stateEl = document.getElementById('save-state');
	const toggleEl = document.getElementById('mode-toggle');
	const editorEl = document.getElementById('editor');
	const readEl = document.getElementById('read-view');
	const publishEl = document.getElementById('publish-btn');
	const historyEl = document.getElementById('history-view');

	const typeDefs = JSON.parse(document.getElementById('types-data').textContent);
	const TYPES = {};
	const ORDER = [];
	for (const t of typeDefs.types) {
		TYPES[t.name] = t;
		ORDER.push(t.name);
	}

	// Nodes are flat on disk: id, type, links and children are the node's own
	// keys, and everything else is a field. Declared before first use — the
	// initial flatten() below runs at module level.
	const RESERVED = new Set(['id', 'type', 'children', 'links', 'fields', 'items', 'page']);

	const isTaskPage = shell.dataset.taskPage === '1';
	const hasRepo = shell.dataset.hasRepo === '1';

	// tasks maps a task-todo's node id to the state of its working file.
	let tasks = JSON.parse(document.getElementById('tasks-data').textContent || '{}');

	// Same rules as doc.Slug on the server: letters and digits survive,
	// everything else becomes a separator. Norwegian letters are letters, so
	// "Oppgåver" slugs to "oppgåver" — hence deriving these rather than
	// spelling them out, which is how they drifted apart the first time.
	function slugify(v) {
		return String(v || '').toLowerCase().trim()
			.replace(/[^\p{L}\p{N}]+/gu, '-').replace(/^-|-$/g, '');
	}

	const TASKS_HEADING = slugify('Oppgåver');
	const ARCHIVE_HEADING = slugify('Arkiv');

	const docData = JSON.parse(document.getElementById('doc-data').textContent);
	let title = docData.title || '';
	let rows = flatten(docData.children || [], 1, []);
	if (!rows.length) rows = [newRow('text', 1)];

	// ---------------------------------------------------------------- model

	function newId() {
		return 'n_' + Math.random().toString(16).slice(2, 10);
	}

	function typeOf(name) {
		return TYPES[name] || TYPES.text;
	}

	function defaults(typeName) {
		const td = typeOf(typeName);
		const f = {};
		for (const fd of td.fields) {
			if (fd.default !== undefined && fd.default !== null) f[fd.name] = fd.default;
			else if (fd.kind === 'bool') f[fd.name] = false;
			else if (fd.kind === 'number') f[fd.name] = 0;
			else f[fd.name] = '';
		}
		return f;
	}

	function newRow(typeName, depth, item) {
		return {
			id: newId(), type: typeName, fields: defaults(typeName),
			links: null, page: null, depth: depth, item: !!item,
		};
	}

	// holdsItems is true for the types whose nesting lives inside the line
	// (list, todo); holdsChildren is true for headers alone.
	function holdsItems(typeName) {
		const t = TYPES[typeName];
		return !!t && t.nestable && !t.allowsHeaders;
	}
	function holdsChildren(typeName) {
		const t = TYPES[typeName];
		return !!t && t.nestable && t.allowsHeaders;
	}

	function fieldsOf(n) {
		const f = {};
		for (const k of Object.keys(n)) {
			if (!RESERVED.has(k)) f[k] = n[k];
		}
		// Accept the older nested shape so a hand-written file still loads.
		if (n.fields) Object.assign(f, n.fields);
		return f;
	}

	function flatten(nodes, depth, out, itemOf) {
		for (const n of nodes) {
			const type = itemOf || (TYPES[n.type] ? n.type : 'text');
			out.push({
				id: n.id || newId(),
				type: type,
				fields: Object.assign(defaults(type), fieldsOf(n)),
				links: n.links || null,
				// The task page this line owns. Machine-maintained like links:
				// carried through untouched so a save never loses it — dropping
				// it makes the server think the task has no page and open a
				// second one.
				page: n.page || null,
				depth: depth,
				// An item is a sub-line of the line above it, not a line of
				// its own. It is kept in the flat list so the caret can reach
				// it, and folded back into its parent when saving.
				item: !!itemOf,
			});
			if (n.items && n.items.length) flatten(n.items, depth + 1, out, type);
			if (n.children && n.children.length) flatten(n.children, depth + 1, out);
		}
		return out;
	}

	// nest rebuilds the tree from the flat rows. A row's parent is the nearest
	// row above it that sits one level shallower.
	function nest() {
		const root = [];
		const stack = [{ depth: 0, children: root }];
		let host = null; // the line an item belongs to

		for (const r of rows) {
			if (r.item && host) {
				const it = Object.assign({ id: r.id }, coerce(r));
				if (r.links) it.links = r.links;
				if (r.page) it.page = r.page;
				host.items.push(it);
				continue;
			}
			while (stack.length > 1 && stack[stack.length - 1].depth >= r.depth) stack.pop();
			const node = Object.assign({ id: r.id, type: r.type }, coerce(r));
			// Link hints are the server's to maintain; pass them back
			// untouched so it can tell what a query used to point at.
			if (r.links) node.links = r.links;
			if (r.page) node.page = r.page;
			node.items = [];
			node.children = [];
			stack[stack.length - 1].children.push(node);
			stack.push({ depth: r.depth, children: node.children });
			host = TYPES[r.type] && TYPES[r.type].nestable && !TYPES[r.type].allowsHeaders ? node : null;
		}
		return strip(root);
	}

	// coerce turns editor strings back into the kinds the types file declares.
	function coerce(r) {
		const out = {};
		for (const fd of typeOf(r.type).fields) {
			const v = r.fields[fd.name];
			if (fd.kind === 'bool') {
				out[fd.name] = !!v;
			} else if (fd.kind === 'number') {
				const n = parseFloat(String(v == null ? '' : v).replace(',', '.'));
				out[fd.name] = isNaN(n) ? 0 : n;
			} else {
				out[fd.name] = v == null ? '' : String(v);
			}
		}
		return out;
	}

	function strip(nodes) {
		for (const n of nodes) {
			if (n.items && !n.items.length) delete n.items;
			if (n.children && n.children.length) strip(n.children);
			else delete n.children;
		}
		return nodes;
	}

	function canContain(parentType, childType) {
		if (!parentType) return true; // top level of the page
		const p = TYPES[parentType];
		if (!p || !p.nestable) return false;
		if (childType === 'header') return !!p.allowsHeaders;
		return true;
	}

	// blockEnd returns the index just past row i and everything nested under it.
	function blockEnd(i) {
		let j = i + 1;
		while (j < rows.length && rows[j].depth > rows[i].depth) j++;
		return j;
	}

	// prevAt returns the index of the nearest row above i at exactly depth d,
	// without crossing above that depth.
	function prevAt(i, d) {
		for (let j = i - 1; j >= 0; j--) {
			if (rows[j].depth === d) return j;
			if (rows[j].depth < d) return -1;
		}
		return -1;
	}

	// headingsAbove lists the headings a row sits inside, outermost first.
	function headingsAbove(i) {
		const out = [];
		let depth = rows[i].depth;
		for (let j = i - 1; j >= 0 && depth > 1; j--) {
			if (rows[j].type === 'header' && rows[j].depth < depth) {
				out.unshift(rows[j]);
				depth = rows[j].depth;
			}
		}
		return out;
	}

	// inTasks reports whether a row sits under the Oppgåver heading, which is
	// the only place a todo or task may be created.
	function inTasks(i) {
		return headingsAbove(i).some(function (h) {
			return slugify(h.fields.text) === TASKS_HEADING;
		});
	}

	// allowedType reports whether a type may be used at a row's position.
	// Todos live under Oppgåver and nowhere else; tasks open a working file of
	// their own, so a working file cannot contain them.
	function allowedType(i, typeName) {
		if (typeName === 'task' && isTaskPage) return false;
		if (typeName === 'todo' || typeName === 'task') return inTasks(i);
		return true;
	}

	// parentType returns the type of row i's parent, or '' at the top level.
	function parentType(i) {
		const p = prevAt(i, rows[i].depth - 1);
		return p === -1 ? '' : rows[p].type;
	}

	// --------------------------------------------------------------- folding

	// Which headings are folded is a view preference, not content, so it is
	// kept per page in localStorage and never written into the document.
	const collapseKey = 'marksheets:collapsed:' + slug;
	let collapsed = new Set();
	let hasFoldPrefs = false;
	try {
		const stored = localStorage.getItem(collapseKey);
		hasFoldPrefs = stored !== null;
		collapsed = new Set(JSON.parse(stored || '[]'));
	} catch (e) { /* nothing stored, or unreadable */ }

	function rememberCollapsed() {
		try {
			localStorage.setItem(collapseKey, JSON.stringify(Array.from(collapsed)));
		} catch (e) { /* private mode: folding just will not persist */ }
	}

	function childCount(i) {
		return blockEnd(i) - i - 1;
	}

	function isFolded(i) {
		return collapsed.has(rows[i].id) && childCount(i) > 0;
	}

	// visibleRows lists the indices on screen: everything except what sits
	// under a folded heading.
	function visibleRows() {
		const out = [];
		let hideBelow = 0;
		for (let i = 0; i < rows.length; i++) {
			if (hideBelow && rows[i].depth > hideBelow) continue;
			hideBelow = 0;
			out.push(i);
			if (isFolded(i)) hideBelow = rows[i].depth;
		}
		return out;
	}

	// step returns the next visible row index in a direction, or -1 at the end.
	function step(i, dir) {
		const vis = visibleRows();
		const at = vis.indexOf(i);
		if (at === -1) return -1;
		const t = at + dir;
		return t >= 0 && t < vis.length ? vis[t] : -1;
	}

	function toggleFold(r) {
		if (collapsed.has(r.id)) collapsed.delete(r.id);
		else collapsed.add(r.id);
		rememberCollapsed();
		render({ id: r.id, field: typeOf(r.type).primary });
	}

	// --------------------------------------------------- the provisional line

	// provisional holds the id of a line conjured by pressing ↑ at the top of
	// the page. It disappears again the moment you go elsewhere without
	// typing in it, so a stray keypress leaves nothing behind.
	let provisional = null;

	function addLineAbove() {
		if (provisional) return;
		const r = newRow('text', rows.length ? rows[0].depth : 1);
		rows.unshift(r);
		provisional = r.id;
		// Deliberately not marked dirty: a line that may vanish again should
		// not make the page look edited.
		render({ id: r.id, field: typeOf('text').primary, off: 0 });
	}

	// dropProvisional removes the conjured line unless it is the one being
	// moved to, or something has been typed into it. Returns whether the rows
	// changed, so the caller knows to re-render.
	function dropProvisional(keepId) {
		if (!provisional || provisional === keepId) return false;
		const id = provisional;
		provisional = null;
		const i = rows.findIndex(function (r) { return r.id === id; });
		if (i === -1 || rows.length === 1) return false;
		if (String(rows[i].fields[typeOf(rows[i].type).primary] || '') !== '') return false;
		if (childCount(i) > 0) return false;
		rows.splice(i, 1);
		return true;
	}

	// --------------------------------------------------------------- archive

	// findHeading returns the index of a heading by slug, searching only
	// inside the block that starts at `from` (or the whole page for -1).
	function findHeading(slug, from) {
		const start = from === -1 ? 0 : from + 1;
		const limit = from === -1 ? rows.length : blockEnd(from);
		for (let i = start; i < limit; i++) {
			if (rows[i].type === 'header' && slugify(rows[i].fields.text) === slug) return i;
		}
		return -1;
	}

	// moveTask files a finished task under Arkiv, and takes it back out when
	// unticked. Archiving keeps the tasks heading showing what is still to do,
	// with everything done folded away underneath it.
	function moveTask(r, done) {
		const at = rows.indexOf(r);
		const tasksAt = findHeading(TASKS_HEADING, -1);
		if (at === -1 || tasksAt === -1) return;

		const block = rows.splice(at, 1);
		let archiveAt = findHeading(ARCHIVE_HEADING, findHeading(TASKS_HEADING, -1));

		if (done) {
			if (archiveAt === -1) {
				const heading = newRow('header', rows[findHeading(TASKS_HEADING, -1)].depth + 1);
				heading.fields.text = 'Arkiv';
				archiveAt = blockEnd(findHeading(TASKS_HEADING, -1));
				rows.splice(archiveAt, 0, heading);
				collapsed.add(heading.id); // folded by default
				rememberCollapsed();
			}
			block[0].depth = rows[archiveAt].depth + 1;
			rows.splice(blockEnd(archiveAt), 0, block[0]);
			return;
		}

		// Back to the open list: after the last unarchived line of the tasks
		// heading, which is just before Arkiv when it exists.
		const tasksNow = findHeading(TASKS_HEADING, -1);
		block[0].depth = rows[tasksNow].depth + 1;
		const back = archiveAt === -1 ? blockEnd(tasksNow) : findHeading(ARCHIVE_HEADING, tasksNow);
		rows.splice(back, 0, block[0]);
	}

	// --------------------------------------------------------------- history

	// The rows are rebuilt from scratch on every structural edit, which throws
	// away whatever undo state the browser had. Undo therefore works on the
	// document model instead: each step keeps a copy of the rows from before a
	// change, along with where the caret was, so undoing puts both back.
	const HISTORY_LIMIT = 200;
	const COALESCE_MS = 700;

	const past = [];
	const future = [];
	let coalesceKey = null;
	let coalesceAt = 0;

	function snapshot() {
		return {
			title: title,
			rows: rows.map(function (r) {
				return {
					id: r.id,
					type: r.type,
					depth: r.depth,
					item: !!r.item,
					fields: Object.assign({}, r.fields),
					links: r.links ? Object.assign({}, r.links) : null,
					page: r.page || null,
				};
			}),
			focus: focusState(),
		};
	}

	// focusState records the caret so an undo can return it where it was.
	function focusState() {
		const c = here();
		if (c) return { id: c.row.id, field: c.field, off: c.off };
		if (document.activeElement === titleEl) return { title: true, off: caret(titleEl) };
		return null;
	}

	function pushPast(state) {
		past.push(state);
		if (past.length > HISTORY_LIMIT) past.shift();
		future.length = 0;
		coalesceKey = null;
	}

	// editStep runs a structural change and records the state before it. A
	// change that declines to happen (returns false) records nothing.
	function editStep(fn) {
		const before = snapshot();
		if (fn() === false) return false;
		pushPast(before);
		return true;
	}

	// markTyping records one step per run of typing in the same field rather
	// than one per keystroke. A pause, a move to another field, or finishing a
	// word all start a fresh step, so undo comes back in readable chunks.
	function markTyping(key, endsRun) {
		const now = Date.now();
		if (key === coalesceKey && now - coalesceAt < COALESCE_MS) {
			coalesceAt = now;
			if (endsRun) coalesceKey = null;
			return;
		}
		past.push(snapshot());
		if (past.length > HISTORY_LIMIT) past.shift();
		future.length = 0;
		coalesceKey = endsRun ? null : key;
		coalesceAt = now;
	}

	function applySnapshot(state) {
		title = state.title;
		titleEl.textContent = title;
		document.title = (title || 'Utan tittel') + ' — Marksheets';
		rows = state.rows.map(function (r) {
			return {
				id: r.id,
				type: r.type,
				depth: r.depth,
				item: !!r.item,
				fields: Object.assign({}, r.fields),
				links: r.links ? Object.assign({}, r.links) : null,
				page: r.page || null,
			};
		});
		// A conjured line has no meaning across an undo; forget it rather than
		// leave a dangling id behind.
		provisional = null;
		coalesceKey = null;

		if (state.focus && state.focus.title) {
			render(null);
			titleEl.focus();
			setCaret(titleEl, state.focus.off);
		} else {
			render(state.focus);
		}
		dirty();
	}

	function undo() {
		if (!past.length) return;
		future.push(snapshot());
		applySnapshot(past.pop());
	}

	function redo() {
		if (!future.length) return;
		past.push(snapshot());
		applySnapshot(future.pop());
	}

	// resetHistory drops the undo stack, for when the document is replaced under
	// the editor rather than edited in it. Undoing back across a discard would
	// put back rows the file no longer has.
	function resetHistory() {
		past.length = 0;
		future.length = 0;
		coalesceKey = null;
	}

	// ------------------------------------------------------------------ view

	function render(focus) {
		const frag = document.createDocumentFragment();
		for (const i of visibleRows()) frag.appendChild(renderRow(rows[i], i));
		rowsEl.replaceChildren(frag);
		if (focus) restore(focus);
		// Rows are rebuilt from plain text, so the link marks go on again after.
		highlightAll();
	}

	function renderRow(r, i) {
		const td = typeOf(r.type);
		const el = document.createElement('div');
		el.className = 'row row-' + r.type + (r.item ? ' is-item' : '');
		el.dataset.id = r.id;
		el.dataset.type = r.type;
		el.style.setProperty('--depth', String(r.depth - 1));
		if (r.type === 'header') el.dataset.level = String(Math.min(r.depth, 6));
		if (r.type === 'todo' && r.fields.done) el.classList.add('is-done');

		const kids = childCount(i);
		if (kids > 0) {
			const folded = collapsed.has(r.id);
			const twisty = document.createElement('button');
			twisty.type = 'button';
			twisty.className = 'twisty' + (folded ? ' is-folded' : '');
			twisty.textContent = folded ? '▸' : '▾';
			twisty.title = folded ? 'Vis innhaldet' : 'Skjul innhaldet';
			twisty.addEventListener('mousedown', function (e) {
				e.preventDefault();
				toggleFold(r);
			});
			el.appendChild(twisty);
			if (folded) el.classList.add('is-folded');
		} else {
			// A spacer keeps every row's text on the same left edge.
			const gap = document.createElement('span');
			gap.className = 'twisty twisty-gap';
			el.appendChild(gap);
		}

		const gutter = document.createElement('button');
		gutter.type = 'button';
		gutter.className = 'gutter';
		gutter.textContent = td.icon || '·';
		gutter.title = r.item ? 'Underpunkt av linja over' : td.label + ' — klikk for å byte type';
		gutter.addEventListener('mousedown', function (e) {
			e.preventDefault();
			if (!r.item) openTypeMenu(r, gutter);
		});
		el.appendChild(gutter);

		const fields = document.createElement('div');
		fields.className = 'fields';
		for (const fd of td.fields) fields.appendChild(renderField(r, fd));
		if (r.type === 'task') fields.appendChild(taskLink(r));
		if (kids > 0 && collapsed.has(r.id)) {
			const badge = document.createElement('span');
			badge.className = 'fold-count';
			badge.textContent = kids + (kids === 1 ? ' linje' : ' linjer');
			fields.appendChild(badge);
		}
		el.appendChild(fields);
		return el;
	}

	// taskLink opens the task's working file. The text stays plainly editable —
	// a link inside a contenteditable would make click-to-edit and
	// click-to-open the same gesture.
	function taskLink(r) {
		const state = tasks[r.id];
		if (!state) {
			const pending = document.createElement('span');
			pending.className = 'task-open is-pending';
			pending.textContent = '→';
			pending.title = String(r.fields.text || '').trim()
				? 'Lagre (⌘S) for å opprette arbeidssida'
				: 'Skriv inn oppgåva først';
			return pending;
		}
		const a = document.createElement('a');
		a.className = 'task-open' + (state.empty ? '' : ' has-content');
		a.href = '/p/' + state.page;
		a.textContent = '→';
		a.title = state.empty
			? 'Opne arbeidssida (tom)'
			: 'Opne arbeidssida (' + state.lines + ' linjer)';
		a.addEventListener('mousedown', function (e) { e.stopPropagation(); });
		return a;
	}

	// openTask follows a task's link, saving first so the page exists.
	function openTask(r) {
		const state = tasks[r.id];
		if (state) {
			window.location.href = '/p/' + state.page;
			return;
		}
		if (!String(r.fields.text || '').trim()) return;
		Promise.resolve(save()).then(function () {
			const fresh = tasks[r.id];
			if (fresh) window.location.href = '/p/' + fresh.page;
		});
	}

	function renderField(r, fd) {
		if (fd.kind === 'bool') {
			const box = document.createElement('input');
			box.type = 'checkbox';
			box.className = 'f f-bool';
			box.dataset.field = fd.name;
			box.checked = !!r.fields[fd.name];
			box.addEventListener('change', function () {
				pushPast(snapshot());
				r.fields[fd.name] = box.checked;
				if (r.type === 'task' && fd.name === 'done') {
					moveTask(r, box.checked);
					dirty();
					render({ id: r.id, field: typeOf(r.type).primary });
					return;
				}
				box.closest('.row').classList.toggle('is-done', box.checked);
				dirty();
			});
			return box;
		}

		const el = document.createElement('span');
		el.className = 'f f-' + fd.kind;
		el.dataset.field = fd.name;
		el.dataset.kind = fd.kind;
		el.dataset.placeholder = fd.placeholder || fd.label || fd.name;
		el.setAttribute('contenteditable', 'plaintext-only');
		el.setAttribute('spellcheck', fd.kind === 'richtext' ? 'true' : 'false');
		const v = r.fields[fd.name];
		el.textContent = v == null ? '' : String(v);

		el.addEventListener('input', function (e) {
			// Recorded before the model is updated, so the step holds the text
			// as it was. Finishing a word closes the step.
			markTyping('f:' + r.id + ':' + fd.name, e.data === ' ');
			if (provisional === r.id) provisional = null; // typed in: it stays
			r.fields[fd.name] = text(el);
			if (fd.name === typeOf(r.type).primary) shortcut(r, el, fd);
			dirty();
		});
		if (fd.name === 'text') {
			el.addEventListener('click', function (e) {
				if ((e.metaKey || e.ctrlKey) && r.type === 'task') {
					e.preventDefault();
					openTask(r);
				}
			});
		}
		el.addEventListener('paste', function (e) {
			e.preventDefault();
			const t = (e.clipboardData || window.clipboardData).getData('text/plain');
			document.execCommand('insertText', false, t.replace(/\r/g, ''));
		});
		return el;
	}

	// shortcut turns markdown-ish prefixes typed at the start of a line into a
	// type change, the way the toolbar would.
	function shortcut(r, el, fd) {
		if (fd.kind !== 'richtext' || r.item) return;
		const v = r.fields[fd.name];
		const rules = [
			[/^# /, 'header'],
			[/^- /, 'list'],
			[/^\[\] /, isTaskPage ? 'todo' : 'task'],
			[/^\[ \] /, isTaskPage ? 'todo' : 'task'],
			[/^= /, 'data'],
		];
		for (const [re, want] of rules) {
			if (!re.test(v) || r.type === want) continue;
			const at = rows.indexOf(r);
			if (!canContain(parentType(at), want) || !allowedType(at, want)) return;
			r.fields[fd.name] = v.replace(re, '');
			setType(r, want);
			return;
		}
	}

	// ---------------------------------------------------------------- caret

	// text reads a contenteditable field the same way the caret helpers count
	// it, so offsets and stored values never drift apart.
	function text(el) {
		let s = '';
		const w = document.createTreeWalker(el, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT);
		let n;
		while ((n = w.nextNode())) {
			if (n.nodeType === 3) s += n.nodeValue;
			else if (n.nodeName === 'BR') s += '\n';
		}
		return s;
	}

	function caret(el) {
		const sel = window.getSelection();
		if (!sel || !sel.focusNode || !el.contains(sel.focusNode)) return 0;
		let off = 0;
		const w = document.createTreeWalker(el, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT);
		let n;
		while ((n = w.nextNode())) {
			if (n === sel.focusNode) return off + sel.focusOffset;
			if (n.nodeType === 3) off += n.nodeValue.length;
			else if (n.nodeName === 'BR') off += 1;
		}
		return off;
	}

	function setCaret(el, off) {
		const range = document.createRange();
		let seen = 0, placed = false;
		const w = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
		let n;
		while ((n = w.nextNode())) {
			if (seen + n.nodeValue.length >= off) {
				range.setStart(n, Math.max(0, off - seen));
				placed = true;
				break;
			}
			seen += n.nodeValue.length;
		}
		if (!placed) range.selectNodeContents(el), range.collapse(false);
		else range.collapse(true);
		const sel = window.getSelection();
		sel.removeAllRanges();
		sel.addRange(range);
	}

	function here() {
		const el = document.activeElement;
		if (!el || !el.dataset || !el.dataset.field) return null;
		const rowEl = el.closest('.row');
		if (!rowEl) return null;
		const i = rows.findIndex(function (r) { return r.id === rowEl.dataset.id; });
		if (i === -1) return null;
		return { el: el, i: i, row: rows[i], field: el.dataset.field, off: caret(el) };
	}

	function restore(f) {
		const rowEl = rowsEl.querySelector('.row[data-id="' + f.id + '"]');
		if (!rowEl) return;
		let field = f.field;
		if (!field) {
			const r = rows.find(function (x) { return x.id === f.id; });
			field = r ? typeOf(r.type).primary : 'text';
		}
		let el = rowEl.querySelector('[data-field="' + field + '"]');
		if (!el || el.tagName === 'INPUT') el = rowEl.querySelector('.f[contenteditable]');
		if (!el) return;
		el.focus();
		setCaret(el, f.off == null ? text(el).length : f.off);
	}

	function focusRow(id, off) {
		restore({ id: id, field: null, off: off });
	}

	// ------------------------------------------------------------- commands

	function setType(r, want) {
		const i = rows.indexOf(r);
		const old = r.fields;
		r.type = want;
		const f = defaults(want);
		// Carry over any field the new type shares with the old one, so
		// turning a todo into a list keeps its text.
		for (const fd of typeOf(want).fields) {
			if (old[fd.name] !== undefined && old[fd.name] !== '') f[fd.name] = old[fd.name];
		}
		r.fields = f;

		// A type that cannot hold what it currently holds gives its children up
		// to the level above rather than losing them.
		const end = blockEnd(i);
		for (let j = i + 1; j < end; j++) {
			if (!canContain(r.type, rows[j].type)) {
				for (let k = j; k < end; k++) rows[k].depth -= 1;
				break;
			}
		}
		dirty();
		render({ id: r.id, field: typeOf(want).primary });
	}

	// Only two things can be indented, and neither can leave its heading.
	//
	// A header moves between outline levels. A list or todo becomes an item of
	// the line above it — sub-lines live inside their parent, so this creates
	// no new line and no new level. Everything else has no indentation of its
	// own: its position is decided by the heading it sits under.
	function indent(i) {
		const r = rows[i];

		if (r.item) return false; // items are already as deep as it goes

		if (r.type === 'header') {
			const p = prevAt(i, r.depth);
			if (p === -1 || rows[p].type !== 'header') return false;
			const end = blockEnd(i);
			for (let j = i; j < end; j++) rows[j].depth += 1;
			return true;
		}

		if (!holdsItems(r.type)) return false;
		if (blockEnd(i) !== i + 1) return false; // a line with items cannot become one

		// Items do not nest, so the host is the nearest line above that is not
		// itself an item — tabbing under a sub-line joins the same list.
		let h = i - 1;
		while (h >= 0 && rows[h].item) h--;
		if (h < 0) return false;
		const host = rows[h];
		// Same type only: an item takes its parent's type, so a todo becoming
		// an item of a list would lose its checkbox and owner.
		if (host.type !== r.type) return false;

		r.item = true;
		r.depth = host.depth + 1;
		return true;
	}

	function outdent(i) {
		const r = rows[i];

		// An item becomes a line again, directly after the line it belonged to.
		if (r.item) {
			const block = rows.splice(i, 1);
			block[0].item = false;
			let at = i;
			while (at < rows.length && rows[at].item) at++;
			block[0].depth = rows[at - 1] ? hostDepth(at - 1) : block[0].depth;
			rows.splice(at, 0, block[0]);
			return true;
		}

		if (r.type !== 'header' || r.depth <= 1) return false;
		const gp = prevAt(i, r.depth - 2);
		if (r.depth - 2 > 0 && (gp === -1 || rows[gp].type !== 'header')) return false;
		const end = blockEnd(i);
		for (let j = i; j < end; j++) rows[j].depth -= 1;
		return true;
	}

	// hostDepth is the depth of the line an item at index i belongs to.
	function hostDepth(i) {
		for (let j = i; j >= 0; j--) {
			if (!rows[j].item) return rows[j].depth;
		}
		return 1;
	}

	function onEnter(c) {
		const td = typeOf(c.row.type);
		const primary = td.primary;
		const value = String(c.row.fields[primary] || '');

		// Enter on an empty line steps back out, where there is anywhere to
		// step out to: an item becomes a line again, a nested header rises a
		// level.
		if (value.trim() === '' && c.field === primary) {
			if (outdent(c.i)) {
				dirty();
				render({ id: c.row.id, field: typeOf(c.row.type).primary, off: 0 });
				return;
			}
			if (c.row.type !== 'text' && !c.row.item) {
				setType(c.row, 'text');
				return;
			}
		}

		let tail = '';
		if (c.field === primary && c.off < value.length) {
			tail = value.slice(c.off);
			c.row.fields[primary] = value.slice(0, c.off);
		}

		let want, depth, at, asItem;
		if (c.row.item) {
			// Stay in the item list.
			want = c.row.type;
			depth = c.row.depth;
			at = c.i + 1;
			asItem = true;
		} else if (c.row.type === 'header') {
			// A heading's content belongs inside it, so Enter opens the first
			// line of the section rather than a sibling of the heading. This
			// is what replaces tabbing every new line into place.
			want = 'text';
			depth = c.row.depth + 1;
			at = c.i + 1;
			asItem = false;
		} else {
			want = td.continues ? c.row.type : 'text';
			depth = c.row.depth;
			at = blockEnd(c.i);
			asItem = false;
		}

		const added = newRow(want, depth, asItem);
		if (tail) added.fields[typeOf(want).primary] = tail;
		// Keep the owner when continuing a todo — retyping it every line is noise.
		if (want === c.row.type) {
			for (const fd of typeOf(want).fields) {
				if (fd.kind === 'tag' && c.row.fields[fd.name]) added.fields[fd.name] = c.row.fields[fd.name];
			}
		}
		rows.splice(at, 0, added);
		dirty();
		render({ id: added.id, field: typeOf(want).primary, off: 0 });
	}

	// newSection starts a heading beside the one you are in, after everything
	// it contains — Enter opens a line *inside* a heading, so this is how you
	// move on to the next section without tabbing back out.
	function newSection(c) {
		let depth, at;
		if (c.row.type === 'header') {
			depth = c.row.depth;
			at = blockEnd(c.i);
		} else {
			const inside = headingsAbove(c.i);
			if (inside.length) {
				const host = inside[inside.length - 1]; // the innermost one
				const hi = rows.indexOf(host);
				depth = host.depth;
				at = blockEnd(hi);
			} else {
				depth = 1;
				at = blockEnd(c.i);
			}
		}
		const added = newRow('header', depth);
		rows.splice(at, 0, added);
		dirty();
		render({ id: added.id, field: 'text', off: 0 });
	}

	function onBackspace(c) {
		const primary = typeOf(c.row.type).primary;
		if (c.field !== primary || c.off !== 0) return false;
		if (String(c.row.fields[primary] || '') !== '') return false;
		// A task carries a working file. While that file holds anything,
		// deleting the task here would strand it — the page is reachable
		// through this line and nowhere else.
		if (c.row.type === 'task') {
			const state = tasks[c.row.id];
			if (state && !state.empty) {
				setState('Arbeidssida har innhald — tøm henne først', 'is-warn');
				return false;
			}
		}
		if (blockEnd(c.i) !== c.i + 1) return false; // has children — leave it alone
		if (rows.length === 1) return false;

		rows.splice(c.i, 1);
		const prev = rows[Math.max(0, c.i - 1)];
		dirty();
		render({ id: prev.id, field: typeOf(prev.type).primary });
		return true;
	}

	rowsEl.addEventListener('keydown', function (e) {
		const c = here();
		if (!c) return;

		if (e.key === 'Enter' && e.altKey) {
			// Soft line break inside the current line.
			e.preventDefault();
			document.execCommand('insertText', false, '\n');
			return;
		}
		if (e.key === 'Enter' && e.shiftKey) {
			e.preventDefault();
			editStep(function () { newSection(c); });
			return;
		}
		if (e.key === 'Enter') {
			e.preventDefault();
			editStep(function () { onEnter(c); });
			return;
		}
		if (e.key === 'Tab') {
			e.preventDefault();
			const moved = editStep(function () {
				return e.shiftKey ? outdent(c.i) : indent(c.i);
			});
			if (moved) {
				dirty();
				render({ id: c.row.id, field: c.field, off: c.off });
			}
			return;
		}
		if (e.key === 'Backspace') {
			let handled = false;
			editStep(function () {
				handled = onBackspace(c);
				return handled;
			});
			if (handled) e.preventDefault();
			return;
		}
		if ((e.metaKey || e.ctrlKey) && e.key >= '1' && e.key <= '9') {
			const want = ORDER[parseInt(e.key, 10) - 1];
			if (want && !c.row.item && canContain(parentType(c.i), want) && allowedType(c.i, want)) {
				e.preventDefault();
				editStep(function () { setType(c.row, want); });
			}
			return;
		}
		if (e.key === 'ArrowUp' || e.key === 'ArrowDown') {
			const up = e.key === 'ArrowUp';
			const value = text(c.el);

			// A field can hold soft line breaks. Only take over once the caret
			// is on the field's first (or last) line — otherwise let the
			// browser move within the field as normal.
			const onEdgeLine = up
				? value.slice(0, c.off).indexOf('\n') === -1
				: value.slice(c.off).indexOf('\n') === -1;
			if (!onEdgeLine) return;

			e.preventDefault();
			const target = step(c.i, up ? -1 : 1);
			if (target === -1) {
				// Past the top: offer a fresh line rather than a dead key.
				if (up) addLineAbove();
				return;
			}
			// Land at the end of the line, so one press is one move — the
			// browser's own behaviour would first park the caret at the edge
			// of the current field and cost a second press.
			const targetId = rows[target].id;
			if (dropProvisional(targetId)) {
				render({ id: targetId, field: null, off: null });
			} else {
				focusRow(targetId, null);
			}
			return;
		}
	});

	// Clicking into another row, or out of the editor entirely, discards an
	// untouched conjured line.
	rowsEl.addEventListener('focusin', function (e) {
		if (!provisional) return;
		const rowEl = e.target.closest ? e.target.closest('.row') : null;
		const id = rowEl ? rowEl.dataset.id : null;
		if (id === provisional) return;
		const field = e.target.dataset ? e.target.dataset.field : null;
		const off = e.target.dataset && e.target.dataset.field ? caret(e.target) : null;
		if (dropProvisional(id)) render(id ? { id: id, field: field, off: off } : null);
	});

	document.addEventListener('mousedown', function (e) {
		if (!provisional || rowsEl.contains(e.target)) return;
		if (dropProvisional(null)) render(null);
	});

	// ------------------------------------------------------------ type menu

	let menu = null;
	let menuAnchor = null;

	function closeMenu() {
		if (menu) menu.remove();
		menu = null;
		menuAnchor = null;
	}

	function openTypeMenu(r, anchor) {
		if (menuAnchor === anchor) { closeMenu(); return; }
		closeMenu();
		menuAnchor = anchor;
		const i = rows.indexOf(r);
		const pt = parentType(i);
		menu = document.createElement('div');
		menu.className = 'type-menu';
		ORDER.forEach(function (name, n) {
			const td = TYPES[name];
			const b = document.createElement('button');
			b.type = 'button';
			b.className = 'type-menu-item' + (name === r.type ? ' is-current' : '');
			b.disabled = !canContain(pt, name) || !allowedType(i, name);
			b.innerHTML = '<span class="type-menu-icon">' + td.icon + '</span>' + td.label +
				'<span class="type-menu-key">⌘' + (n + 1) + '</span>';
			b.addEventListener('click', function () {
				closeMenu();
				editStep(function () { setType(r, name); });
			});
			menu.appendChild(b);
		});
		const box = anchor.getBoundingClientRect();
		menu.style.top = (box.bottom + window.scrollY + 4) + 'px';
		menu.style.left = (box.left + window.scrollX) + 'px';
		document.body.appendChild(menu);
	}

	document.addEventListener('click', function (e) {
		if (!menu) return;
		if (menu.contains(e.target)) return;
		if (menuAnchor && menuAnchor.contains(e.target)) return;
		closeMenu();
	});
	document.addEventListener('keydown', function (e) {
		if (e.key === 'Escape') closeMenu();
	});

	// ------------------------------------------------- links and completion

	// The @ that starts a query may not follow a letter or digit — the same
	// rule the server applies, so an email address never opens the menu and
	// never lights up as a link.
	const WORDCHAR = /[\p{L}\p{N}_]/u;

	// Pages to complete the first segment against. Fetched once, and again
	// whenever the window regains focus, so a page made in another tab turns
	// up without a reload.
	let pageList = [];
	function loadPages() {
		return fetch('/sider.json', { headers: { 'Accept': 'application/json' } })
			.then(function (res) { return res.ok ? res.json() : []; })
			.then(function (list) {
				pageList = list || [];
				// Other pages may have changed while we were away.
				looked.clear();
				highlightAll();
			})
			.catch(function () { /* completion is a convenience; carry on without */ });
	}

	function knownPage(slug) {
		return pageList.some(function (p) { return p.slug === slug; });
	}

	// ---- asking the server about a path ----

	// Anything past the first segment is resolved by the server, not here.
	// Matching a segment means "a direct child, else a descendant deeper
	// down", and a second copy of that rule in this file would drift from the
	// one in query.go the first time either changed. Answers are cached: the
	// same handful of paths is checked on every marking pass.
	const looked = new Map();  // '@side/bolk' -> { ok, born }
	const asking = new Map();  // the same, while in flight

	function lookup(key) {
		if (looked.has(key)) return Promise.resolve(looked.get(key));
		if (asking.has(key)) return asking.get(key);
		const p = fetch('/oppslag.json?q=' + encodeURIComponent(key))
			.then(function (res) { return res.ok ? res.json() : { ok: false, born: [] }; })
			.catch(function () { return { ok: false, born: [] }; })
			.then(function (info) {
				looked.set(key, info);
				asking.delete(key);
				return info;
			});
		asking.set(key, p);
		return p;
	}

	// forget drops what we knew about one page, for after saving it: its own
	// headings may have moved under any link that points into it.
	function forget(pageSlug) {
		for (const k of Array.from(looked.keys())) {
			if (slugify(k.slice(1).split(/[./]/)[0] || '') === pageSlug) looked.delete(k);
		}
	}

	// ---- marking ----

	// A query that resolves is drawn as a link while you type, so you can see
	// that it took. The page is checked here against the list; anything deeper
	// is checked by asking, and stays unmarked until the answer arrives —
	// which is the same as how it looks a keystroke earlier, so nothing
	// flickers. Nothing is ever marked *broken*: while you are still typing
	// @s, @sk, @ska, not resolving yet is the ordinary state.
	const QUERY = /@([\p{L}\p{N}_\-./#]+)(\[[^\]]*\])?(\([^)]*\))?/gu;

	function esc(t) {
		return t.replace(/[&<>]/g, function (c) {
			return c === '&' ? '&amp;' : c === '<' ? '&lt;' : '&gt;';
		}).replace(/\n/g, '<br>');
	}

	function pathResolves(key, el) {
		if (looked.has(key)) return looked.get(key).ok;
		lookup(key).then(function () { highlightNow(el); });
		return false;
	}

	// marked builds the field's HTML with every resolving query wrapped, or
	// null when there is nothing to mark.
	function marked(s, el) {
		let out = '', last = 0, any = false, m;
		QUERY.lastIndex = 0;
		while ((m = QUERY.exec(s))) {
			const at = m.index;
			if (at > 0 && WORDCHAR.test(s[at - 1])) continue;
			const trimmed = m[1].replace(/[./-]+$/, '');
			if (!trimmed) continue;
			const intact = trimmed.length === m[1].length;
			// A trailing full stop ends the query, and then whatever followed
			// it is prose — the same rule parseQuery uses on the server.
			const whole = '@' + trimmed + (intact ? (m[2] || '') + (m[3] || '') : '');
			const segs = trimmed.split(/[./]/).filter(Boolean);
			if (!knownPage(slugify(segs[0] || ''))) continue;
			if (segs.length > 1 && !pathResolves('@' + trimmed, el)) continue;
			out += esc(s.slice(last, at)) + '<span class="lnk">' + esc(whole) + '</span>';
			last = at + whole.length;
			QUERY.lastIndex = last;
			any = true;
		}
		if (!any) return null;
		return out + esc(s.slice(last));
	}

	function highlightNow(el) {
		if (!el || !el.isConnected || el.dataset.kind !== 'richtext') return;
		const s = text(el);
		const html = s === '' ? '' : marked(s, el);
		// Nothing to mark and nothing marked before: leave the node alone
		// rather than rewriting it for no reason.
		if (html === null && !el.querySelector('.lnk')) return;
		const want = html === null ? esc(s) : html;
		if (el.innerHTML === want) return;
		const focused = document.activeElement === el;
		const off = focused ? caret(el) : -1;
		el.innerHTML = want;
		if (focused) setCaret(el, off);
	}

	// Marking runs a moment after typing stops rather than on every keystroke:
	// it replaces the field's nodes, and doing that under a moving caret on
	// every character is how a contenteditable starts fighting you.
	let markTimer = null, markEl = null;
	function highlightSoon(el) {
		markEl = el;
		if (markTimer) clearTimeout(markTimer);
		markTimer = setTimeout(function () {
			markTimer = null;
			if (!menuOpen()) highlightNow(markEl);
		}, 300);
	}

	function highlightAll() {
		for (const el of rowsEl.querySelectorAll('.f-richtext')) highlightNow(el);
	}

	// ---- completion ----

	let comp = null, compToken = 0;
	function menuOpen() { return comp !== null; }

	// prefixAt finds the @-prefix the caret sits at the end of, split into the
	// part already settled and the segment still being typed.
	//
	// Only `/` separates a segment here, though a query may also be written
	// with dots: `@hytta.` at the end of a sentence would otherwise open the
	// list of sections every time someone finished a thought.
	function prefixAt(el) {
		const off = caret(el);
		const s = text(el).slice(0, off);
		const at = s.lastIndexOf('@');
		if (at === -1) return null;
		if (at > 0 && WORDCHAR.test(s[at - 1])) return null;
		const typed = s.slice(at + 1);
		if (!/^[\p{L}\p{N}_\-/]*$/u.test(typed)) return null;
		const cut = typed.lastIndexOf('/');
		const scope = cut >= 0 ? typed.slice(0, cut) : '';
		const tail = cut >= 0 ? typed.slice(cut + 1) : typed;
		// A page needs a letter to go on; past the first slash, everything
		// under the scope is worth offering straight away.
		if (!scope && !tail) return null;
		return { at: at, typed: typed, scope: scope, tail: tail };
	}

	function suggest(p) {
		if (!p.scope) {
			const q = slugify(p.tail);
			return Promise.resolve(pageList.filter(function (pg) {
				return pg.slug.indexOf(q) === 0 || slugify(pg.title).indexOf(q) === 0;
			}).slice(0, 8).map(function (pg) {
				return { insert: '@' + pg.slug, title: pg.title, hint: '@' + pg.slug };
			}));
		}
		return lookup('@' + p.scope).then(function (info) {
			if (!info.ok) return [];
			const q = slugify(p.tail);
			return info.born.filter(function (c) {
				// Headings and data lines are what people address. A line of
				// body text is addressable too, but nobody writes
				// @side/ei-heil-setning.
				if (c.type !== 'header' && c.type !== 'data') return false;
				return !q || c.slug.indexOf(q) === 0 || slugify(c.label).indexOf(q) === 0;
			}).slice(0, 8).map(function (c) {
				return { insert: '@' + p.scope + '/' + c.slug, title: c.label, hint: c.slug };
			});
		});
	}

	function closeComplete() {
		if (!comp) return;
		comp.box.remove();
		comp = null;
	}

	function openComplete(el, p) {
		const token = ++compToken;
		suggest(p).then(function (items) {
			if (token !== compToken) return; // a later keystroke has overtaken this
			if (!items.length) { closeComplete(); return; }
			if (!comp) {
				const box = document.createElement('div');
				box.className = 'complete-menu';
				document.body.appendChild(box);
				comp = { box: box, index: 0 };
			}
			comp.el = el; comp.at = p.at; comp.typed = p.typed; comp.items = items;
			if (comp.index >= items.length) comp.index = 0;
			drawComplete();
			placeComplete(el);
		});
	}

	function drawComplete() {
		comp.box.replaceChildren();
		comp.items.forEach(function (item, i) {
			const b = document.createElement('button');
			b.type = 'button';
			b.className = 'complete-item' + (i === comp.index ? ' is-current' : '');
			const t = document.createElement('span');
			t.className = 'complete-title';
			t.textContent = item.title;
			const c = document.createElement('code');
			c.className = 'complete-slug';
			c.textContent = item.hint;
			b.append(t, c);
			// mousedown, not click: the field must not lose focus first.
			b.addEventListener('mousedown', function (e) { e.preventDefault(); applyComplete(i); });
			comp.box.appendChild(b);
		});
	}

	function placeComplete(el) {
		let box = null;
		const sel = window.getSelection();
		if (sel && sel.rangeCount) {
			const r = sel.getRangeAt(0).cloneRange();
			r.collapse(true);
			box = r.getClientRects()[0];
		}
		if (!box) box = el.getBoundingClientRect();
		comp.box.style.top = (box.bottom + window.scrollY + 4) + 'px';
		comp.box.style.left = (box.left + window.scrollX) + 'px';
	}

	function applyComplete(i) {
		if (!comp) return;
		const item = comp.items[i == null ? comp.index : i];
		const el = comp.el, at = comp.at, typed = comp.typed;
		const c = here();
		if (!item || !c) { closeComplete(); return; }
		const full = text(el);
		const before = full.slice(0, at) + item.insert;
		pushPast(snapshot());
		el.textContent = before + full.slice(at + 1 + typed.length);
		c.row.fields[el.dataset.field] = text(el);
		setCaret(el, before.length);
		closeComplete();
		dirty();
		highlightNow(el);
	}

	rowsEl.addEventListener('input', function (e) {
		const el = e.target;
		if (!el.dataset || el.dataset.kind !== 'richtext') return;
		if (e.isComposing) return;
		const p = prefixAt(el);
		if (p) openComplete(el, p); else { compToken++; closeComplete(); }
		highlightSoon(el);
	});

	rowsEl.addEventListener('focusout', function (e) {
		compToken++;
		closeComplete();
		highlightNow(e.target);
	});

	// Captured, so the menu answers these keys before the editor does — Tab
	// would otherwise indent the line out from under the list.
	document.addEventListener('keydown', function (e) {
		if (!comp) return;
		if (e.key === 'Escape') {
			e.preventDefault(); e.stopPropagation();
			compToken++;
			closeComplete();
			return;
		}
		if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
			e.preventDefault(); e.stopPropagation();
			const n = comp.items.length;
			comp.index = (comp.index + (e.key === 'ArrowDown' ? 1 : n - 1)) % n;
			drawComplete();
			return;
		}
		if (e.key === 'Tab') {
			e.preventDefault(); e.stopPropagation();
			applyComplete(null);
		}
	}, true);

	loadPages();
	window.addEventListener('focus', loadPages);

	// ----------------------------------------------------------------- save

	let pending = false;
	// unpublished is work that is on disk but that nobody else can see yet. It
	// survives a reload, so the server tells us where we stand on page load.
	let unpublished = shell.dataset.unpublished === '1';
	let autosaveTimer = null;
	const AUTOSAVE_MS = 1200;
	const draftKey = 'marksheets:draft:' + slug;

	function setState(text, cls) {
		stateEl.textContent = text;
		stateEl.className = 'save-state' + (cls ? ' ' + cls : '');
	}

	// showState reports the resting position: what is on disk, and whether
	// anyone else can see it. Only called when nothing is in flight.
	function showState() {
		if (pending) return;
		if (!hasRepo) { setState('Lagra'); return; }
		if (unpublished) setState('Lagra · ikkje publisert', 'is-unpublished');
		else setState('Publisert');
		markControls();
	}

	function markControls() {
		if (publishEl) publishEl.disabled = !unpublished;
	}

	// dirty marks work not yet written and schedules the write. A copy still
	// goes to localStorage: autosave is a timer, and the gap between the last
	// keystroke and the next tick is exactly where a crash would land.
	function dirty() {
		pending = true;
		setState('Skriv…', 'is-dirty');
		if (publishEl) publishEl.disabled = true;
		try {
			localStorage.setItem(draftKey, JSON.stringify({ title: title, children: nest(), at: Date.now() }));
		} catch (e) { /* private mode, or full: the beforeunload warning still applies */ }
		if (autosaveTimer) clearTimeout(autosaveTimer);
		autosaveTimer = setTimeout(function () { autosaveTimer = null; save(); }, AUTOSAVE_MS);
	}

	function clearDraft() {
		try { localStorage.removeItem(draftKey); } catch (e) { /* nothing to do */ }
	}

	function save() {
		if (autosaveTimer) { clearTimeout(autosaveTimer); autosaveTimer = null; }
		if (dropProvisional(null)) render(null);
		if (!pending) return Promise.resolve();
		setState('Lagrar…');
		const body = JSON.stringify({ title: title, children: nest() });
		return fetch('/p/' + slug, {
			method: 'PUT',
			headers: { 'Content-Type': 'application/json' },
			body: body,
		}).then(function (res) {
			if (!res.ok) throw new Error(res.statusText);
			return res.json();
		}).then(function (info) {
			pending = false;
			clearDraft();
			if (info.unpublished) unpublished = true;
			forget(slug);
			if (info.tasks) {
				// Re-rendering throws away the caret, whatever menu is open and the
				// link marks. Saving used to be something you asked for; now it
				// happens every second or so while you type, so only rebuild when
				// the task states have actually moved.
				const moved = JSON.stringify(info.tasks) !== JSON.stringify(tasks);
				tasks = info.tasks;
				if (moved) render(focusState());
			}
			if (info.warning) setState(info.warning, 'is-warn');
			else if (info.note) setState('Lagra · ' + info.note, 'is-unpublished');
			else showState();
			markControls();
			// The server rewrites link text when a heading is renamed, so
			// reload the rows from what it actually stored.
			if (info.relinked) reload();
		}).catch(function (err) {
			setState('Lagring feila', 'is-error');
			console.error(err);
		});
	}

	// publish is the deliberate half: commit what is on disk and send it. It
	// saves first, because publishing what is not yet written would publish the
	// previous version and say it had succeeded.
	function publish() {
		if (!hasRepo) return Promise.resolve();
		return Promise.resolve(save()).then(function () {
			if (pending) return; // the save failed; it already said so
			setState('Publiserer…');
			if (publishEl) publishEl.disabled = true;
			return fetch('/p/' + slug + '/publiser', { method: 'POST' }).then(function (res) {
				if (!res.ok) return res.text().then(function (t) { throw new Error(t.trim() || res.statusText); });
				return res.json();
			}).then(function (info) {
				if (info.published) {
					unpublished = false;
					setState('Publisert');
				} else if (info.pushError) {
					// Committed, so the work is in the history; it just has not
					// left this machine. Still unpublished, and still says so.
					setState('Commita, men ikkje sendt', 'is-warn');
					console.error(info.pushError);
				} else {
					unpublished = false;
					setState(info.note || 'Lagra i historikk', 'is-warn');
				}
				markControls();
			}).catch(function (err) {
				setState('Publisering feila', 'is-error');
				console.error(err);
				markControls();
			});
		});
	}

	// restore brings an old version back. It writes the content in as an
	// ordinary unpublished change rather than touching history, so every commit
	// made since is still there and going back is a step forward.
	function restoreVersion(hash, btn) {
		if (!window.confirm('Hente tilbake denne versjonen? Endringar som ikkje er publiserte, går tapt.')) return;
		if (autosaveTimer) { clearTimeout(autosaveTimer); autosaveTimer = null; }
		pending = false;
		btn.disabled = true;
		setState('Hentar tilbake…');
		fetch('/p/' + slug + '/gjenopprett/' + hash, { method: 'POST' }).then(function (res) {
			if (!res.ok) return res.text().then(function (t) { throw new Error(t.trim() || res.statusText); });
			return res.json();
		}).then(function (info) {
			clearDraft();
			if (info.unpublished) unpublished = true;
			// The document was replaced under the editor rather than edited in
			// it, so undoing back across this would restore rows the file no
			// longer has.
			resetHistory();
			return reload().then(function () {
				showState();
				if (reading) window.htmx.ajax('GET', '/p/' + slug + '/view', { target: '#read-view', swap: 'innerHTML' });
			});
		}).catch(function (err) {
			setState(String(err.message || err), 'is-error');
			console.error(err);
			btn.disabled = false;
		});
	}

	// reload pulls the stored document back, after the server has changed it.
	function reload() {
		return fetch('/p/' + slug + '/doc', { headers: { 'Accept': 'application/json' } })
			.then(function (res) { return res.json(); })
			.then(function (d) {
				if (d.tasks) tasks = d.tasks;
				title = d.title || '';
				titleEl.textContent = title;
				rows = flatten(d.children || [], 1, []);
				if (!rows.length) rows = [newRow('text', 1)];
				render({ id: rows[0].id, field: typeOf(rows[0].type).primary, off: 0 });
			});
	}

	document.addEventListener('keydown', function (e) {
		if (!(e.metaKey || e.ctrlKey)) return;
		const key = e.key.toLowerCase();

		if (key === 's') {
			e.preventDefault();
			publish();
			return;
		}
		// Undo is ours, not the browser's: preventing the default keeps a
		// single history rather than one per contenteditable field.
		if (key === 'z' && !editorEl.hidden) {
			e.preventDefault();
			if (e.shiftKey) redo();
			else undo();
			return;
		}
		if (key === 'y' && !editorEl.hidden) {
			e.preventDefault();
			redo();
		}
	});

	titleEl.addEventListener('input', function (e) {
		markTyping('title', e.data === ' ');
		title = text(titleEl).replace(/\n/g, ' ').trim();
		document.title = (title || 'Utan tittel') + ' — Marksheets';
		dirty();
	});
	titleEl.addEventListener('keydown', function (e) {
		if (e.key === 'Enter') {
			e.preventDefault();
			focusRow(rows[0].id, 0);
		}
	});

	if (publishEl) publishEl.addEventListener('click', function () { publish(); });

	// The restore button arrives with a version HTMX swaps in after this script
	// has run, so the click is caught on the panel that holds it.
	if (historyEl) historyEl.addEventListener('click', function (e) {
		const btn = e.target.closest('[data-restore]');
		if (btn) restoreVersion(btn.dataset.restore, btn);
	});

	// Autosave runs on a timer, so leaving with work still in the gap between
	// the last keystroke and the next tick is the one case left to warn about.
	window.addEventListener('beforeunload', function (e) {
		if (!pending) return;
		e.preventDefault();
		e.returnValue = '';
	});

	// A draft newer than the file means the last session ended with unsaved
	// work. Offer it rather than silently discarding or silently applying it.
	function offerDraft() {
		let draft = null;
		try { draft = JSON.parse(localStorage.getItem(draftKey) || 'null'); } catch (e) { return; }
		if (!draft || !draft.children) return;
		if (JSON.stringify(draft.children) === JSON.stringify(nest()) && draft.title === title) {
			clearDraft();
			return;
		}
		const bar = document.createElement('div');
		bar.className = 'draft-bar';
		bar.innerHTML = '<span>Du har ulagra endringar frå sist.</span>';

		const restore = document.createElement('button');
		restore.type = 'button';
		restore.className = 'btn';
		restore.textContent = 'Hent dei fram';
		restore.addEventListener('click', function () {
			title = draft.title || '';
			titleEl.textContent = title;
			rows = flatten(draft.children, 1, []);
			if (!rows.length) rows = [newRow('text', 1)];
			render({ id: rows[0].id, field: typeOf(rows[0].type).primary, off: 0 });
			bar.remove();
			dirty();
		});

		const discard = document.createElement('button');
		discard.type = 'button';
		discard.className = 'btn-ghost';
		discard.textContent = 'Forkast';
		discard.addEventListener('click', function () {
			clearDraft();
			bar.remove();
		});

		bar.append(restore, discard);
		editorEl.prepend(bar);
	}

	// ----------------------------------------------------------- read/edit

	let reading = false;

	toggleEl.addEventListener('click', function () {
		if (reading) {
			reading = false;
			readEl.hidden = true;
			editorEl.hidden = false;
			toggleEl.textContent = 'Les';
			return;
		}
		// Save first: the read view is rendered server-side from stored data,
		// and @-queries must see what is on screen.
		Promise.resolve(save()).then(function () {
			reading = true;
			editorEl.hidden = true;
			readEl.hidden = false;
			toggleEl.textContent = 'Rediger';
			window.htmx.ajax('GET', '/p/' + slug + '/view', { target: '#read-view', swap: 'innerHTML' });
		});
	});

	// ------------------------------------------------------------------ go

	// Finished work starts out of the way. Only until you say otherwise —
	// once you fold or unfold anything on this page, your choice is stored and
	// this no longer applies.
	if (!hasFoldPrefs) {
		for (const r of rows) {
			if (r.type === 'header' && slugify(r.fields.text) === ARCHIVE_HEADING) collapsed.add(r.id);
		}
	}

	render({ id: rows[0].id, field: typeOf(rows[0].type).primary, off: 0 });
	showState();
	offerDraft();
})();
