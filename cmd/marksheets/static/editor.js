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
	const tagsEl = document.getElementById('doc-tags');

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
	const RESERVED = new Set(['id', 'type', 'children', 'links', 'fields', 'items', 'page',
		'columns', 'rows']);

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

	const TASKS_LABEL = 'Oppgåver';
	const TASKS_HEADING = slugify(TASKS_LABEL);
	const ARCHIVE_HEADING = slugify('Arkiv');

	// The line the tasks heading holds: a task on an ordinary page, where it
	// opens a working file of its own, and a plain todo on a working file,
	// which cannot open working files at all.
	const TASK_TYPE = isTaskPage ? 'todo' : 'task';

	const docData = JSON.parse(document.getElementById('doc-data').textContent);
	let title = docData.title || '';
	// The page's own hashtags. Doc-level, like the title: they say what the
	// page is about rather than what any one line says.
	let tags = (docData.tags || []).slice();
	let rows = flatten(docData.children || [], 1, []);
	ensurePinned();

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
			// A number starts empty rather than at zero: a fresh data line
			// showing "0" is a value nobody typed, and it makes "is this line
			// blank" unanswerable. coerce turns an empty field back into 0 on
			// the way to disk, so nothing downstream sees the difference.
			else f[fd.name] = '';
		}
		return f;
	}

	function newRow(typeName, depth, item) {
		const r = {
			id: newId(), type: typeName, fields: defaults(typeName),
			links: null, page: null, depth: depth, item: !!item,
			// A table's shape. Null on every other type, and machine-carried
			// like links and page: they have to survive flatten → nest and the
			// undo snapshots, or a save quietly empties the table.
			columns: null, tableRows: null,
		};
		if (typeName === 'table') fixTable(r);
		return r;
	}

	// fixTable makes a table rectangular: at least one column and one row, and
	// every row as wide as the columns. The server repairs a table the same way
	// on load; this is here for the one document that never goes past it, a
	// draft coming back out of localStorage.
	function fixTable(r) {
		if (!r.columns || !r.columns.length) r.columns = ['', ''];
		if (!r.tableRows || !r.tableRows.length) r.tableRows = [{ id: newId(), cells: [] }];
		for (const tr of r.tableRows) {
			if (!tr.id) tr.id = newId();
			if (!tr.cells) tr.cells = [];
			while (tr.cells.length < r.columns.length) tr.cells.push('');
			tr.cells.length = r.columns.length;
		}
	}

	// tableHasContent reports whether a table holds anything a person typed.
	// Its content is in cells rather than fields, so an empty name field says
	// nothing about it.
	function tableHasContent(r) {
		if (r.type !== 'table' || !r.columns) return false;
		if (r.columns.some(function (v) { return String(v || '').trim() !== ''; })) return true;
		return (r.tableRows || []).some(function (tr) {
			return tr.cells.some(function (v) { return String(v || '').trim() !== ''; });
		});
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
				columns: n.columns ? n.columns.slice() : null,
				tableRows: n.rows ? n.rows.map(function (t) {
					return { id: t.id || newId(), cells: (t.cells || []).slice() };
				}) : null,
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
			if (r.type === 'table') {
				node.columns = (r.columns || []).slice();
				node.rows = (r.tableRows || []).map(function (t) {
					return { id: t.id, cells: t.cells.slice() };
				});
			}
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
	//
	// Under Oppgåver, tasks and nothing else: the heading *is* the task list,
	// and a stray text line or sub-heading in it would make it something else.
	// Everywhere else a task is the one thing that may not be made, because its
	// working file belongs to a page rather than to another working file.
	//
	// Like every rule here this is enforced on creation only. Lines that
	// already sit somewhere they could not now be made keep working untouched.
	function allowedType(i, typeName) {
		if (inTasks(i)) return typeName === TASK_TYPE;
		return typeName !== 'todo' && typeName !== 'task';
	}

	// -------------------------------------------------------- pinned heading

	// The tasks heading is furniture rather than content: pinned to the top of
	// every page, never typed in, renamed or moved, and left out of the read
	// view. The server pins it on every load and every save, so this is here
	// for the one document that never goes past the server — a draft coming
	// back out of localStorage.
	function isTasksRow(r) {
		return r.type === 'header' && slugify(r.fields.text) === TASKS_HEADING;
	}

	// pinned is the heading row itself. Only row 0 counts: a section further
	// down that happens to be called "Oppgåver" is somebody's own heading.
	function pinned() {
		return rows.length && isTasksRow(rows[0]) ? rows[0] : null;
	}

	function ensurePinned() {
		const at = rows.findIndex(function (r) { return isTasksRow(r) && r.depth === 1; });
		if (at === 0) return;
		if (at === -1) {
			const heading = newRow('header', 1);
			heading.fields.text = TASKS_LABEL;
			rows.unshift(heading, newRow(TASK_TYPE, 2));
			return;
		}
		// Moved rather than copied, so the tasks already written come with it.
		rows = rows.splice(at, blockEnd(at) - at).concat(rows);
	}

	// Where the caret goes when a page opens: the first line *after* the tasks,
	// which is where the page itself starts. Opening a page to write on it is
	// the common case, and reviewing the task list is the one you scroll up
	// half a screen for.
	//
	// The whole pinned block is stepped over, not just its heading — the tasks
	// are its contents, and landing among them would be landing in the part of
	// the page that is not the page.
	function firstCaretRow() {
		const vis = visibleRows();
		const body = pinned() ? blockEnd(0) : 0;
		for (const i of vis) {
			if (i >= body) return i;
		}
		// Nothing on this page but its tasks. Better the first of those than
		// no caret at all.
		for (const i of vis) {
			if (i === 0 && pinned()) continue;
			return i;
		}
		return -1;
	}

	function focusFirst() {
		const i = firstCaretRow();
		if (i === -1) render(null);
		else render({ id: rows[i].id, field: typeOf(rows[i].type).primary, off: 0 });
	}

	// addTask puts a new task at the end of the open list, before the Arkiv
	// that holds the ones already finished. The heading carries the button that
	// calls this because it has no caret to press Enter in — without it,
	// ticking or deleting the last open task would leave nowhere to write the
	// next one.
	function addTask() {
		if (!pinned()) return false;
		const arch = findHeading(ARCHIVE_HEADING, 0);
		const at = arch === -1 ? blockEnd(0) : arch;
		const added = newRow(TASK_TYPE, rows[0].depth + 1);
		rows.splice(at, 0, added);
		// A folded heading would swallow the line that was just asked for.
		collapsed.delete(rows[0].id);
		rememberCollapsed();
		dirty();
		render({ id: added.id, field: typeOf(TASK_TYPE).primary, off: 0 });
	}

	// editableFields are the fields of a type you can put a caret in: every
	// kind but bool, which is drawn as a checkbox.
	function editableFields(typeName) {
		return typeOf(typeName).fields.filter(function (fd) { return fd.kind !== 'bool'; });
	}

	// isBlank reports whether a line holds nothing at all — every field, not
	// just the primary one. A data line is three fields wide, so asking after
	// the name alone would call a row with a value in it empty.
	function isBlank(r) {
		return editableFields(r.type).every(function (fd) {
			const v = r.fields[fd.name];
			return String(v == null ? '' : v).trim() === '';
		});
	}

	// A line with more than one field and no indentation of its own — a data
	// line, an image — uses Tab to move between its fields, the way a form
	// does. Nothing is competing for the key there: Tab cannot indent it.
	function tabsBetweenFields(r) {
		return !typeOf(r.type).nestable && editableFields(r.type).length > 1;
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
	// The pinned heading is passed over: it holds no field, so the caret has
	// nowhere to land there.
	function step(i, dir) {
		const vis = visibleRows();
		const at = vis.indexOf(i);
		if (at === -1) return -1;
		for (let t = at + dir; t >= 0 && t < vis.length; t += dir) {
			if (vis[t] === 0 && pinned()) continue;
			return vis[t];
		}
		return -1;
	}

	function toggleFold(r) {
		if (collapsed.has(r.id)) collapsed.delete(r.id);
		else collapsed.add(r.id);
		rememberCollapsed();
		render({ id: r.id, field: typeOf(r.type).primary });
	}

	// bodyStart is the index the page proper begins at, past the tasks.
	function bodyStart() {
		return pinned() ? blockEnd(0) : 0;
	}

	// ------------------------------------------------------- block selection

	// A selection of whole lines, as a range between the line it started on and
	// the line it has reached. Lines rather than characters: what you do with
	// several lines at once is move, copy or delete them, and none of those
	// wants half a line.
	//
	// It is held as row ids rather than indices so that a re-render — which
	// happens on every structural edit — cannot silently shift what is
	// selected out from under it.
	let selAnchor = null, selHead = null;

	// selection returns the selected range as indices, or null. Ids that no
	// longer exist mean the selection was edited away.
	function selection() {
		if (!selAnchor || !selHead) return null;
		const a = rows.findIndex(function (r) { return r.id === selAnchor; });
		const b = rows.findIndex(function (r) { return r.id === selHead; });
		if (a === -1 || b === -1) return null;
		return { from: Math.min(a, b), to: Math.max(a, b) };
	}

	function clearSelection() {
		if (!selAnchor && !selHead) return;
		selAnchor = selHead = null;
		paintSelection();
	}

	// paintSelection marks the rows without rebuilding them: a selection is a
	// view state, and re-rendering the document to show one would throw away
	// the caret and every open menu with it.
	function paintSelection() {
		const sel = selection();
		rowsEl.classList.toggle('is-selecting', !!sel);
		for (const el of rowsEl.querySelectorAll('.row')) {
			const i = rows.findIndex(function (r) { return r.id === el.dataset.id; });
			el.classList.toggle('is-selected', !!sel && i >= sel.from && i <= sel.to);
		}
	}

	// extendSelection grows the range by one visible line. The caret stays
	// where it is: moving it would collapse the browser's own selection and
	// take this one with it, and there is nothing to type into a block anyway.
	function extendSelection(i, dir) {
		if (!selAnchor) { selAnchor = rows[i].id; selHead = rows[i].id; }
		const sel = selection();
		if (!sel) { selAnchor = selHead = null; return; }
		const head = rows.findIndex(function (r) { return r.id === selHead; });
		const vis = visibleRows();
		const at = vis.indexOf(head);
		const to = at + dir;
		if (at === -1 || to < 0 || to >= vis.length) return;
		selHead = rows[vis[to]].id;
		paintSelection();
	}

	// The mouse route in, and it has to be drawn by hand.
	//
	// Every field is its own contenteditable, which makes it its own editing
	// host: the browser will not extend a selection out of one and into the
	// next, however far you drag. There is no cross-row selection to read, so
	// the drag is tracked directly — which row it started in, which row the
	// pointer is over now — and the block is the span between them.
	//
	// The native selection inside the row it started in is left alone rather
	// than cleared. Clearing it would take the caret with it, and the caret is
	// what the keyboard needs to know which lines are meant; the CSS hides its
	// paint instead, so one gesture shows as one highlight.
	let dragFrom = null;

	rowsEl.addEventListener('mousedown', function (e) {
		if (e.button !== 0) return;
		clearSelection();
		const rowEl = e.target.closest ? e.target.closest('.row') : null;
		dragFrom = rowEl ? rowEl.dataset.id : null;
	});

	document.addEventListener('mousemove', function (e) {
		if (!dragFrom || !e.buttons) return;
		const under = document.elementFromPoint(e.clientX, e.clientY);
		const rowEl = under && under.closest ? under.closest('.row') : null;
		if (!rowEl || !rowsEl.contains(rowEl)) return;
		const id = rowEl.dataset.id;
		if (id === dragFrom) {
			// Back where it started: one line is not a block.
			clearSelection();
			return;
		}
		selAnchor = dragFrom;
		selHead = id;
		paintSelection();
	});

	document.addEventListener('mouseup', function () { dragFrom = null; });

	// ----------------------------------------------------------- clipboard

	// What was last copied from here, as rows. The clipboard itself carries
	// plain text — that is what another program can read — and this keeps the
	// structure beside it. A paste whose text is exactly what we wrote is the
	// same lines coming home, so they come back whole; anything else is text
	// from elsewhere and is read as text.
	let copied = null; // { text, rows }

	// asText writes a block the way somebody would type it: the type as a
	// markdown-ish prefix, the depth as indentation. Legible in a mail, and
	// close enough to the line-start shortcuts to come back in as itself.
	function asText(from, to) {
		const out = [];
		for (let i = from; i <= to; i++) {
			const r = rows[i];
			const pad = '  '.repeat(Math.max(0, r.depth - 1 + (r.item ? 1 : 0)));
			const t = String(r.fields[typeOf(r.type).primary] || '');
			if (r.type === 'header') out.push(pad + '#'.repeat(Math.min(r.depth, 6)) + ' ' + t);
			else if (r.type === 'list') out.push(pad + '- ' + t);
			else if (r.type === 'ordered') out.push(pad + ordinalOf(i) + '. ' + t);
			else if (r.type === 'todo' || r.type === 'task') {
				out.push(pad + (r.fields.done ? '- [x] ' : '- [ ] ') + t);
			} else if (r.type === 'data') {
				out.push(pad + t + ': ' + [r.fields.value, r.fields.unit].join(' ').trim());
			} else if (r.type === 'table') {
				out.push(pad + r.columns.join('\t'));
				for (const tr of r.tableRows) out.push(pad + tr.cells.join('\t'));
			} else if (r.type === 'image') {
				out.push(pad + '![' + (r.fields.alt || '') + '](' + (r.fields.src || '') + ')');
			} else out.push(pad + t);
		}
		return out.join('\n');
	}

	// cloneRows copies rows for the clipboard. Ids are dropped rather than
	// carried: a pasted line is a new line, and two lines sharing an id would
	// make every @-query pointing at one of them ambiguous.
	function cloneRows(from, to) {
		const out = [];
		for (let i = from; i <= to; i++) {
			const r = rows[i];
			out.push({
				type: r.type, depth: r.depth, item: !!r.item,
				fields: Object.assign({}, r.fields),
				columns: r.columns ? r.columns.slice() : null,
				tableRows: r.tableRows ? r.tableRows.map(function (t) {
					return { cells: t.cells.slice() };
				}) : null,
			});
		}
		return out;
	}

	// blockCanGo reports whether a selected block may be taken away, and says
	// why not. The same guards as deleting one line at a time: nothing that
	// owns content elsewhere leaves without being emptied first.
	function blockCanGo(sel) {
		for (let i = sel.from; i <= sel.to; i++) {
			const r = rows[i];
			if (i === 0 && pinned()) return 'Oppgåver-overskrifta kan ikkje fjernast';
			if (r.type === 'task') {
				const st = tasks[r.id];
				if (st && !st.empty) return 'Ei av oppgåvene har ei arbeidsside med innhald';
			}
			if (r.type === 'table' && tableHasContent(r)) return 'Ein av tabellane har innhald';
		}
		return '';
	}

	function copySelection(e, cut) {
		const sel = selection();
		if (!sel) return false;
		const text = asText(sel.from, sel.to);
		copied = { text: text, rows: cloneRows(sel.from, sel.to) };
		if (e.clipboardData) e.clipboardData.setData('text/plain', text);
		e.preventDefault();
		if (!cut) return true;

		const why = blockCanGo(sel);
		if (why) { setState(why + ' — kan ikkje klippast ut', 'is-warn'); return true; }
		editStep(function () {
			rows.splice(sel.from, sel.to - sel.from + 1);
			clearSelection();
			if (!rows.length) rows.push(newRow('text', 1));
			const land = rows[Math.min(sel.from, rows.length - 1)];
			dirty();
			render({ id: land.id, field: typeOf(land.type).primary, off: 0 });
			return true;
		});
		return true;
	}

	// pasteRows drops a copied block in after the current line. The rows are
	// rebuilt rather than reused, so a second paste of the same block is not
	// the same lines twice.
	function pasteRows(c, block) {
		const at = blockEnd(c.i);
		const made = [];
		for (const b of block) {
			const r = newRow(b.type, b.depth, b.item);
			r.fields = Object.assign(defaults(b.type), b.fields);
			if (b.columns) {
				r.columns = b.columns.slice();
				r.tableRows = b.tableRows.map(function (t) {
					return { id: newId(), cells: t.cells.slice() };
				});
			}
			made.push(r);
		}
		if (!made.length) return false;
		rows.splice.apply(rows, [at, 0].concat(made));
		clearSelection();
		dirty();
		const last = made[made.length - 1];
		render({ id: last.id, field: typeOf(last.type).primary });
		return true;
	}

	// linesToRows reads plain text from anywhere else, taking the line-start
	// shortcuts at face value — the same prefixes asText writes, so a round
	// trip through another program still comes back as lines rather than one
	// long paragraph.
	function linesToRows(text, depth) {
		const out = [];
		for (const raw of text.replace(/\r/g, '').split('\n')) {
			const line = raw.trim();
			if (line === '') continue;
			let m;
			if ((m = /^(#{1,6})\s+(.*)$/.exec(line))) {
				out.push({ type: 'header', depth: m[1].length, item: false, fields: { text: m[2] } });
			} else if ((m = /^- \[([ xX])\]\s+(.*)$/.exec(line))) {
				out.push({ type: 'todo', depth: depth, item: false,
					fields: { done: m[1].toLowerCase() === 'x', text: m[2] } });
			} else if ((m = /^[-*]\s+(.*)$/.exec(line))) {
				out.push({ type: 'list', depth: depth, item: false, fields: { text: m[1] } });
			} else if ((m = /^\d+[.)]\s+(.*)$/.exec(line))) {
				out.push({ type: 'ordered', depth: depth, item: false, fields: { text: m[1] } });
			} else {
				out.push({ type: 'text', depth: depth, item: false, fields: { text: line } });
			}
		}
		return out;
	}

	document.addEventListener('copy', function (e) { copySelection(e, false); });
	document.addEventListener('cut', function (e) { copySelection(e, true); });

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
			tags: tags.slice(),
			rows: rows.map(function (r) {
				return {
					id: r.id,
					type: r.type,
					depth: r.depth,
					item: !!r.item,
					fields: Object.assign({}, r.fields),
					links: r.links ? Object.assign({}, r.links) : null,
					page: r.page || null,
					columns: r.columns ? r.columns.slice() : null,
					tableRows: r.tableRows ? r.tableRows.map(function (t) {
						return { id: t.id, cells: t.cells.slice() };
					}) : null,
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
		tags = (state.tags || []).slice();
		renderTags();
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
				columns: r.columns ? r.columns.slice() : null,
				tableRows: r.tableRows ? r.tableRows.map(function (t) {
					return { id: t.id, cells: t.cells.slice() };
				}) : null,
			};
		});
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
		// Every structural edit ends here, so this is the one place the depth
		// rule has to hold — see reflow. Doing it on the way to the screen
		// means an invalid shape cannot be drawn even once.
		reflow();
		const body = pinned() ? bodyStart() : -1;
		const frag = document.createDocumentFragment();
		let box = null;
		for (const i of visibleRows()) {
			const el = renderRow(rows[i], i);
			// The tasks go in a box of their own: they are the page's working
			// state rather than the page, and the outline below should not
			// read as a continuation of the task list.
			if (body !== -1 && i < body) {
				if (!box) {
					box = document.createElement('div');
					box.className = 'tasks-box';
					frag.appendChild(box);
				}
				box.appendChild(el);
			} else {
				if (i === body) el.classList.add('after-tasks');
				frag.appendChild(el);
			}
		}
		rowsEl.replaceChildren(frag);
		paintSelection();
		if (focus) restore(focus);
		// Rows are rebuilt from plain text, so the link marks go on again after.
		highlightAll();
	}

	function renderRow(r, i) {
		const td = typeOf(r.type);
		const isPinned = i === 0 && isTasksRow(r);
		const el = document.createElement('div');
		el.className = 'row row-' + r.type + (r.item ? ' is-item' : '') + (isPinned ? ' is-pinned' : '');
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
		// A numbered line shows the number it will carry rather than its type
		// icon. It is counted from the run the line sits in and never stored,
		// so inserting one in the middle renumbers the rest by itself.
		gutter.textContent = r.type === 'ordered' ? ordinalOf(i) + '.' : (td.icon || '·');
		gutter.title = isPinned ? 'Fast overskrift — oppgåvene på sida'
			: r.item ? 'Underpunkt av linja over'
			: td.label + ' — klikk for å byte type';
		gutter.addEventListener('mousedown', function (e) {
			e.preventDefault();
			if (!r.item && !isPinned) openTypeMenu(r, gutter);
		});
		el.appendChild(gutter);

		const fields = document.createElement('div');
		fields.className = 'fields';
		if (isPinned) {
			// A label, not a field: the heading is the machine's, so there is
			// nothing here to put a caret in and nothing to type over.
			const label = document.createElement('span');
			label.className = 'pinned-title';
			label.textContent = TASKS_LABEL;
			fields.append(label, addTaskButton());
		} else {
			for (const fd of td.fields) fields.appendChild(renderField(r, fd));
			if (r.type === 'task') fields.appendChild(taskLink(r));
			if (r.type === 'table') fields.appendChild(renderTable(r));
		}
		if (kids > 0 && collapsed.has(r.id)) {
			const badge = document.createElement('span');
			badge.className = 'fold-count';
			badge.textContent = kids + (kids === 1 ? ' linje' : ' linjer');
			fields.appendChild(badge);
		}
		el.appendChild(fields);
		return el;
	}

	// A table is one line of the document and one grid on screen. Its cells
	// carry a position rather than a field name — `col:2` for a heading,
	// `cell:1:2` for a body cell — which is what lets here(), restore() and the
	// undo focus address them like any other field, without a second caret
	// mechanism just for tables.
	function renderTable(r) {
		const grid = document.createElement('div');
		grid.className = 'tbl';
		grid.style.setProperty('--cols', String(r.columns.length));
		r.columns.forEach(function (name, ci) {
			grid.appendChild(tableCell(r, 'col:' + ci, name, true));
		});
		r.tableRows.forEach(function (tr, ri) {
			for (let ci = 0; ci < r.columns.length; ci++) {
				grid.appendChild(tableCell(r, 'cell:' + ri + ':' + ci, tr.cells[ci] || '', false));
			}
		});
		return grid;
	}

	function tableCell(r, field, value, head) {
		const el = document.createElement('span');
		el.className = 'tbl-cell f' + (head ? ' tbl-head' : '');
		el.dataset.field = field;
		// Deliberately not richtext: the marker, the @-completion and the read
		// view all agree that a cell holds plain text with inline markdown, and
		// no queries. See render.table for why.
		el.dataset.kind = 'text';
		if (head) el.dataset.placeholder = 'Kolonne';
		el.setAttribute('contenteditable', 'plaintext-only');
		el.setAttribute('spellcheck', 'false');
		el.textContent = value;
		el.addEventListener('input', function () {
			markTyping('t:' + r.id + ':' + field, false);
			writeCell(r, field, text(el));
			dirty();
		});
		el.addEventListener('paste', function (e) {
			e.preventDefault();
			const t = (e.clipboardData || window.clipboardData).getData('text/plain');
			document.execCommand('insertText', false, t.replace(/[\r\n]/g, ' '));
		});
		return el;
	}

	// tableAt reads a cell's position back out of its field name.
	function tableAt(field) {
		let m = /^col:(\d+)$/.exec(field || '');
		if (m) return { head: true, col: +m[1] };
		m = /^cell:(\d+):(\d+)$/.exec(field || '');
		if (m) return { head: false, row: +m[1], col: +m[2] };
		return null;
	}

	function writeCell(r, field, value) {
		const at = tableAt(field);
		if (!at) return;
		if (at.head) r.columns[at.col] = value;
		else if (r.tableRows[at.row]) r.tableRows[at.row].cells[at.col] = value;
	}

	// focusCell puts the caret in one cell. A row of -1 is the heading row.
	function focusCell(r, ri, ci) {
		restore({ id: r.id, field: ri < 0 ? 'col:' + ci : 'cell:' + ri + ':' + ci, off: null });
	}

	// addTaskButton starts a new task from the pinned heading — see addTask.
	function addTaskButton() {
		const b = document.createElement('button');
		b.type = 'button';
		b.className = 'add-task';
		b.textContent = '+';
		b.title = 'Ny ' + typeOf(TASK_TYPE).label.toLowerCase();
		b.addEventListener('mousedown', function (e) {
			e.preventDefault();
			editStep(function () { addTask(); });
		});
		return b;
	}

	// ordinalOf counts a numbered line's position in the run it belongs to: the
	// lines of the same type directly above it, at the same level and the same
	// side of the item boundary.
	function ordinalOf(i) {
		let count = 1;
		for (let j = i - 1; j >= 0; j--) {
			const p = rows[j];
			if (p.type !== 'ordered' || p.depth !== rows[i].depth || !!p.item !== !!rows[i].item) break;
			count++;
		}
		return count;
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
			// A contenteditable emptied by Backspace keeps a stray <br>, which
			// reads back as "\n" — so a line that looks empty holds a newline,
			// and every rule that asks "is this line empty" quietly answers no.
			// Nothing downstream wants that distinction: a field holding only
			// whitespace is an empty field, and is stored as one.
			const typed = text(el);
			r.fields[fd.name] = typed.trim() === '' ? '' : typed;
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
			const t = (e.clipboardData || window.clipboardData).getData('text/plain');
			e.preventDefault();
			// Several lines become several lines. A block copied from here
			// comes back as itself — same types, same depths, same table
			// contents — and anything else is read through the line-start
			// prefixes. One line is just text, and goes in where the caret is.
			if (/\n/.test(t.trim())) {
				const c = here();
				if (c) {
					const block = copied && copied.text === t ? copied.rows : linesToRows(t, c.row.depth);
					if (editStep(function () { return pasteRows(c, block); })) return;
				}
			}
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
			[/^\d+[.)] /, 'ordered'],
			[/^\[\] /, TASK_TYPE],
			[/^\[ \] /, TASK_TYPE],
			[/^= /, 'data'],
			[/^\| /, 'table'],
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

		// The caret can sit on the field itself rather than in a text node
		// inside it — an emptied contenteditable holding nothing but Chrome's
		// placeholder <br> is the everyday case. focusOffset is then an index
		// into childNodes, and the walker below would never meet the node to
		// match it against: it would run to the end and report the length of
		// the field as the caret position.
		//
		// That is how Backspace stopped deleting a heading you had just
		// emptied. The line was empty, but the caret read as being at offset 1
		// — past the placeholder — so onBackspace declined. Reloading the page
		// rebuilt the field from plain text, with no placeholder and nothing
		// to miscount, which is why it worked on the second try.
		if (sel.focusNode === el) {
			for (let i = 0; i < sel.focusOffset && i < el.childNodes.length; i++) {
				const n = el.childNodes[i];
				if (n.nodeType === 3) off += n.nodeValue.length;
				else if (n.nodeName === 'BR') off += 1;
				else off += text(n).length;
			}
			return off;
		}

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

	// changeType is the model half: it changes what a line is and leaves the
	// screen alone, so a command that has more to do can do it before drawing.
	function changeType(r, want) {
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
	}

	function setType(r, want) {
		// A table's content is in its cells, and no other type has anywhere to
		// put them. Refuse rather than drop them on the next save — the same
		// bargain as a task whose working file still holds work.
		if (r.type === 'table' && want !== 'table' && tableHasContent(r)) {
			setState('Tabellen har innhald — tøm henne først', 'is-warn');
			return;
		}
		changeType(r, want);
		dirty();
		render({ id: r.id, field: typeOf(want).primary });
	}

	// reflow puts every line back where its heading says it belongs.
	//
	// Depth belongs to headings alone. A leaf has none of its own: it sits
	// exactly one level inside the heading above it, and a leaf standing
	// *beside* a heading is a shape nobody can type but everybody can arrive
	// at — write a line first, then put a heading above it, and there it is.
	// Rather than police every edit for it, the depths are simply recomputed
	// after each one, which is cheap and cannot be got wrong in one place and
	// right in another.
	//
	// The pinned heading encloses its tasks and nothing else. It is furniture
	// rather than a section of the page, so the line after it is back at the
	// top level rather than inside anything.
	function reflow() {
		const body = bodyStart();
		let inside = 0;
		for (let i = 0; i < rows.length; i++) {
			const r = rows[i];
			if (i === body) inside = 0;
			if (r.type === 'table') fixTable(r);
			if (r.item) {
				r.depth = hostDepth(i) + 1;
			} else if (r.type === 'header') {
				inside = r.depth;
			} else {
				r.depth = inside + 1;
			}
		}
	}

	// Tab means "one level in", and what that costs depends on what the line is.
	//
	// A header moves between outline levels. A list or todo becomes an item of
	// the line above it — sub-lines live inside their parent, so this creates
	// no new line and no new level. A text line has no indentation of its own,
	// so the only way it can go in a level is to *become* the heading that
	// holds the level: the same thing typing `#` does, from wherever the caret
	// already is.
	function indent(i) {
		const r = rows[i];

		if (r.item) return false; // items are already as deep as it goes

		if (r.type === 'text') {
			if (!allowedType(i, 'header') || !canContain(parentType(i), 'header')) return false;
			changeType(r, 'header');
			return true;
		}

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

	// outLevel turns an empty line into the heading of the next section, one
	// level out from where it sat. A heading already is one, so it just rises;
	// anything else becomes one where it stands and then rises with it.
	//
	// At the top level there is nowhere further out, but becoming a heading is
	// still the useful half — that is how the blank line a new page opens on
	// turns into the page's first section.
	function outLevel(i) {
		const r = rows[i];
		if (!allowedType(i, 'header')) return false;
		if (r.type !== 'header') {
			if (!canContain(parentType(i), 'header')) return false;
			changeType(r, 'header');
		}
		outdent(i); // no-op at depth 1, where being a heading is enough
		return true;
	}

	// hostDepth is the depth of the line an item at index i belongs to.
	function hostDepth(i) {
		for (let j = i; j >= 0; j--) {
			if (!rows[j].item) return rows[j].depth;
		}
		return 1;
	}

	// ------------------------------------------------------------- the table

	// Inside a table the keys mean what they mean in a spreadsheet, not what
	// they mean in the outline. tableKey takes them and reports whether it did.
	function tableKey(c, e) {
		const at = tableAt(c.field);
		if (!at) {
			// The name field above the grid: ↓ goes into the table rather than
			// stepping over the whole thing.
			if (e.key === 'ArrowDown' && c.field === 'name') {
				e.preventDefault();
				focusCell(c.row, -1, 0);
				return true;
			}
			return false;
		}
		if (e.key === 'Tab') {
			e.preventDefault();
			editStep(function () { return tableTab(c, at, e.shiftKey); });
			return true;
		}
		if (e.key === 'Enter' && !e.altKey) {
			e.preventDefault();
			editStep(function () { return tableEnter(c, at); });
			return true;
		}
		if (e.key === 'ArrowUp' || e.key === 'ArrowDown') {
			e.preventDefault();
			tableMove(c, at, e.key === 'ArrowUp' ? -1 : 1);
			return true;
		}
		// Backspace in an empty heading takes the column away, but only when
		// nothing in it would go with it.
		if (e.key === 'Backspace' && at.head && c.off === 0 && text(c.el) === '') {
			if (c.row.columns.length > 1 && columnIsEmpty(c.row, at.col)) {
				e.preventDefault();
				editStep(function () { return dropColumn(c.row, at.col); });
				return true;
			}
		}
		return false;
	}

	// Tab walks the table left to right, headings first. Off the right-hand
	// edge it makes a new column rather than stopping: that is how a table
	// grows, and it is the only gesture that adds one.
	function tableTab(c, at, back) {
		const r = c.row;
		const cols = r.columns.length;
		const ri = at.head ? -1 : at.row;

		if (!back && at.col + 1 >= cols) {
			r.columns.push('');
			for (const tr of r.tableRows) tr.cells.push('');
			dirty();
			render(null);
			focusCell(r, ri, cols);
			return true;
		}
		if (back && at.col === 0) {
			// Off the left edge: the end of the row above, or the table's own
			// name when there is no row above.
			if (ri <= 0 && at.head) {
				restore({ id: r.id, field: 'name', off: null });
			} else {
				focusCell(r, ri - 1, cols - 1);
			}
			return false;
		}
		focusCell(r, ri, at.col + (back ? -1 : 1));
		return false; // moving the caret is not an edit
	}

	// Enter opens the next row. On a row that is blank in every cell it leaves
	// the table instead — a blank row means the table is finished, the same
	// rule a blank data line follows, and what comes after a table is prose.
	function tableEnter(c, at) {
		const r = c.row;
		if (at.head) {
			focusCell(r, 0, at.col);
			return false;
		}
		if (rowIsBlank(r, at.row)) {
			if (r.tableRows.length > 1) r.tableRows.splice(at.row, 1);
			const added = newRow('text', r.depth);
			rows.splice(c.i + 1, 0, added);
			dirty();
			render({ id: added.id, field: 'text', off: 0 });
			return true;
		}
		r.tableRows.splice(at.row + 1, 0, {
			id: newId(),
			cells: r.columns.map(function () { return ''; }),
		});
		dirty();
		render(null);
		focusCell(r, at.row + 1, 0);
		return true;
	}

	// ↑ and ↓ move within the table before leaving it, so a table neither
	// swallows the caret nor gets jumped over in a single press.
	function tableMove(c, at, dir) {
		const r = c.row;
		const to = (at.head ? -1 : at.row) + dir;
		if (to >= 0 && to < r.tableRows.length) { focusCell(r, to, at.col); return; }
		if (to === -1) { focusCell(r, -1, at.col); return; }
		if (dir < 0) { restore({ id: r.id, field: 'name', off: null }); return; }
		const next = step(c.i, 1);
		if (next !== -1) focusRow(rows[next].id, null);
	}

	function rowIsBlank(r, ri) {
		const tr = r.tableRows[ri];
		return !tr || tr.cells.every(function (v) { return String(v || '').trim() === ''; });
	}

	function columnIsEmpty(r, ci) {
		if (String(r.columns[ci] || '').trim() !== '') return false;
		return r.tableRows.every(function (tr) {
			return String(tr.cells[ci] || '').trim() === '';
		});
	}

	function dropColumn(r, ci) {
		if (r.columns.length <= 1) return false;
		r.columns.splice(ci, 1);
		for (const tr of r.tableRows) tr.cells.splice(ci, 1);
		dirty();
		render(null);
		focusCell(r, -1, Math.max(0, ci - 1));
		return true;
	}

	// stepField moves the caret across the fields of a line, and on past the
	// end of it. Returns whether it took the key.
	function stepField(c, back) {
		if (!tabsBetweenFields(c.row)) return false;
		const names = editableFields(c.row.type).map(function (fd) { return fd.name; });
		const at = names.indexOf(c.field);
		if (at === -1) return false;

		const to = at + (back ? -1 : 1);
		if (to >= 0 && to < names.length) {
			restore({ id: c.row.id, field: names[to], off: null });
			return true;
		}

		// Off the end of the line. Tab carries on to the next one the way it
		// moves through a form, rather than stopping at the edge of a row.
		const next = step(c.i, back ? -1 : 1);
		if (next === -1) return true; // nowhere to go — but Tab must not indent
		const r = rows[next];
		const nf = editableFields(r.type);
		if (!nf.length) return true;
		restore({ id: r.id, field: (back ? nf[nf.length - 1] : nf[0]).name, off: null });
		return true;
	}

	function onEnter(c) {
		const td = typeOf(c.row.type);
		const primary = td.primary;
		const value = String(c.row.fields[primary] || '');

		// Enter on an empty line starts the next section: the line becomes a
		// heading one level out from where it sat. An empty line is somebody
		// who has finished what they were writing, and what follows that is
		// nearly always a new heading — so rather than leave a blank line
		// behind and make them type `#`, the blank line *is* the heading.
		//
		// This replaces the brief's double-Enter, and the old rule that turned
		// an emptied line back into a text line. Neither survives it: there is
		// no way now to leave an empty line lying in a document.
		// A data line is a row of a table, and a blank row means the table is
		// finished. What follows a table is prose far more often than a new
		// section, so this one type leaves an empty line as a text line rather
		// than turning it into the heading every other empty line becomes.
		if (c.row.type === 'data' && isBlank(c.row) && allowedType(c.i, 'text')) {
			setType(c.row, 'text');
			return;
		}

		if (value.trim() === '' && c.field === primary) {
			// An item steps out of its list first — that is a level of its own,
			// and a sub-line is not a section waiting to happen.
			if (c.row.item) {
				if (outdent(c.i)) {
					dirty();
					render({ id: c.row.id, field: typeOf(c.row.type).primary, off: 0 });
				}
				return;
			}
			// Under Oppgåver there are no headings to make, and nowhere to
			// step out to. An empty task stays one rather than breeding
			// another empty task below it.
			if (inTasks(c.i)) return;
			if (outLevel(c.i)) {
				dirty();
				render({ id: c.row.id, field: 'text', off: 0 });
			}
			return;
		}

		// Enter at the very start makes room above: the line keeps what it
		// holds and moves down, leaving an empty line where it stood. That is
		// what pressing Enter at the start of a line does everywhere, and it is
		// what replaced conjuring a line with ↑ at the top of the page.
		//
		// A heading takes its whole section down with it. Splitting a heading
		// at offset zero used to empty it and drop its own title into a text
		// line *inside* it — the split is only a split when there is something
		// on the left of the caret to keep.
		//
		// The line is moved rather than copied: it keeps its id, and with it
		// the working file a task owns and the link hints recorded on it.
		// Emptying it and putting its text in a fresh line below looks the same
		// on screen and quietly separates the text from everything attached to
		// it.
		if (c.off === 0 && c.field === primary) {
			// Room made above a line that continues is another line of the same
			// kind: above a list item you want a list item, above a table row a
			// table row. Under Oppgåver it has to be a task either way, and a
			// sub-line stays a sub-line of the same list.
			const want = (c.row.item || td.continues || !allowedType(c.i, 'text'))
				? c.row.type : 'text';
			const added = newRow(want, c.row.depth, c.row.item);
			if (want === c.row.type) {
				for (const fd of typeOf(want).fields) {
					if (fd.kind === 'tag' && c.row.fields[fd.name]) added.fields[fd.name] = c.row.fields[fd.name];
				}
			}
			rows.splice(c.i, 0, added);
			dirty();
			render({ id: c.row.id, field: primary, off: 0 });
			return;
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

	// deleteSelection takes the selected lines, or says why it will not.
	function deleteSelection() {
		const sel = selection();
		if (!sel) return;
		const why = blockCanGo(sel);
		if (why) { setState(why + ' — kan ikkje slettast', 'is-warn'); return; }
		editStep(function () {
			rows.splice(sel.from, sel.to - sel.from + 1);
			clearSelection();
			if (!rows.length) rows.push(newRow('text', 1));
			const land = rows[Math.min(sel.from, rows.length - 1)];
			dirty();
			render({ id: land.id, field: typeOf(land.type).primary, off: 0 });
			return true;
		});
	}

	function onBackspace(c) {
		const primary = typeOf(c.row.type).primary;
		if (c.field !== primary || c.off !== 0) return false;
		if (String(c.row.fields[primary] || '').trim() !== '') return false;
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
		// An empty name says nothing about a table: its content is in cells.
		if (c.row.type === 'table' && tableHasContent(c.row)) {
			setState('Tabellen har innhald — tøm henne først', 'is-warn');
			return false;
		}
		if (rows.length === 1) return false;

		if (blockEnd(c.i) !== c.i + 1) {
			// Only a heading gives its contents up. Deleting one deletes the
			// heading, never what was under it: the lines it held move up a
			// level and join whatever the heading itself belonged to. This is
			// doc.Normalise's rule for orphans, applied to the one gesture
			// that makes them — content is never lost to keep a tree tidy.
			if (c.row.type !== 'header') return false;
			const end = blockEnd(c.i);
			for (let j = c.i + 1; j < end; j++) rows[j].depth -= 1;
		}

		rows.splice(c.i, 1);
		const prev = rows[Math.max(0, c.i - 1)];
		dirty();
		render({ id: prev.id, field: typeOf(prev.type).primary });
		return true;
	}

	rowsEl.addEventListener('keydown', function (e) {
		const c = here();
		if (!c) return;

		// ⇧↑ and ⇧↓ take whole lines. Only from the edge of the field, the same
		// rule the plain arrows follow, so selecting text inside a line still
		// works where there is text above or below the caret to select.
		if ((e.key === 'ArrowUp' || e.key === 'ArrowDown') && e.shiftKey) {
			const up = e.key === 'ArrowUp';
			const v = text(c.el);
			const onEdge = up ? v.slice(0, c.off).indexOf('\n') === -1
				: v.slice(c.off).indexOf('\n') === -1;
			if (selection() || onEdge) {
				e.preventDefault();
				extendSelection(c.i, up ? -1 : 1);
				return;
			}
		}

		// Backspace or Delete with lines selected takes the lines.
		if ((e.key === 'Backspace' || e.key === 'Delete') && selection()) {
			e.preventDefault();
			deleteSelection();
			return;
		}

		// Any other key is somebody carrying on typing, and the block goes.
		if (!e.metaKey && !e.ctrlKey && !e.altKey && e.key !== 'Shift') clearSelection();

		// A table answers most of these keys itself before the outline sees
		// them: inside a grid, Tab and Enter mean cell and row.
		if (c.row.type === 'table' && tableKey(c, e)) return;

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
		if (e.key === 'Tab' && stepField(c, e.shiftKey)) {
			// Moving the caret is not an edit, so no undo step and nothing to
			// re-render.
			e.preventDefault();
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
			if (target === -1) return;
			// Land at the end of the line, so one press is one move — the
			// browser's own behaviour would first park the caret at the edge
			// of the current field and cost a second press.
			focusRow(rows[target].id, null);
			return;
		}
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
		ORDER.forEach(function (name) {
			const td = TYPES[name];
			const b = document.createElement('button');
			b.type = 'button';
			b.className = 'type-menu-item' + (name === r.type ? ' is-current' : '');
			b.disabled = !canContain(pt, name) || !allowedType(i, name);
			b.innerHTML = '<span class="type-menu-icon">' + td.icon + '</span>' + td.label;
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
			localStorage.setItem(draftKey, JSON.stringify({ title: title, tags: tags, children: nest(), at: Date.now() }));
		} catch (e) { /* private mode, or full: the beforeunload warning still applies */ }
		if (autosaveTimer) clearTimeout(autosaveTimer);
		autosaveTimer = setTimeout(function () { autosaveTimer = null; save(); }, AUTOSAVE_MS);
	}

	function clearDraft() {
		try { localStorage.removeItem(draftKey); } catch (e) { /* nothing to do */ }
	}

	function save() {
		if (autosaveTimer) { clearTimeout(autosaveTimer); autosaveTimer = null; }
		if (!pending) return Promise.resolve();
		setState('Lagrar…');
		const body = JSON.stringify({ title: title, tags: tags, children: nest() });
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
			// The store guarantees a page has at least one tag, so a document
			// that arrived without any comes back carrying one.
			if (info.tags && JSON.stringify(info.tags) !== JSON.stringify(tags)) {
				tags = info.tags;
				renderTags();
			}
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
				tags = d.tags || [];
				renderTags();
				titleEl.textContent = title;
				rows = flatten(d.children || [], 1, []);
				ensurePinned();
				focusFirst();
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
			const i = firstCaretRow();
			if (i !== -1) focusRow(rows[i].id, 0);
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
		const draftTags = draft.tags || tags;
		if (JSON.stringify(draft.children) === JSON.stringify(nest()) && draft.title === title &&
			JSON.stringify(draftTags) === JSON.stringify(tags)) {
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
			tags = draftTags.slice();
			renderTags();
			titleEl.textContent = title;
			rows = flatten(draft.children, 1, []);
			ensurePinned();
			focusFirst();
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

	// ---------------------------------------------------------------- tags

	// A page's hashtags say what it is about, and the home page lists a page by
	// them where it used to show the file name. They belong to the page rather
	// than to any line on it, so they are edited here, beside the title, and
	// travel with `title` through saving, drafts and undo.

	// Tags are written as words and stored as slugs — the same normalising the
	// server does — so «Ved til vinteren» and «ved-til-vinteren» are one tag,
	// and it does not matter whether you separate them with spaces or commas.
	function tagsIn(v) {
		const out = [];
		for (const part of String(v || '').split(/[\s,;#]+/)) {
			const tag = slugify(part);
			if (tag && out.indexOf(tag) === -1) out.push(tag);
		}
		return out;
	}

	function renderTags() {
		if (!tagsEl) return;
		const frag = document.createDocumentFragment();
		for (const tag of tags) {
			const chip = document.createElement('span');
			chip.className = 'tag-chip';
			chip.append('#' + tag);
			const drop = document.createElement('button');
			drop.type = 'button';
			drop.className = 'tag-drop';
			drop.textContent = '×';
			drop.title = 'Fjern emneknaggen';
			// mousedown with the default prevented: a click would blur the
			// field first, rebuild the chips, and remove the button from under
			// the pointer before it ever fired.
			drop.addEventListener('mousedown', function (e) {
				e.preventDefault();
				dropTag(tag);
			});
			chip.appendChild(drop);
			frag.appendChild(chip);
		}
		const add = document.createElement('span');
		add.className = 'tag-add';
		add.dataset.placeholder = tags.length ? '+ emneknagg' : 'Legg til ein emneknagg';
		add.setAttribute('contenteditable', 'plaintext-only');
		add.setAttribute('spellcheck', 'false');
		add.addEventListener('keydown', onTagKey);
		// Leaving the field keeps what was in it. Half-typed and walked away
		// from is still what you meant, and dropping it silently would be worse.
		add.addEventListener('blur', function () { addTags(add); });
		frag.appendChild(add);
		tagsEl.replaceChildren(frag);
	}

	function focusTagField() {
		const add = tagsEl && tagsEl.querySelector('.tag-add');
		if (add) add.focus();
	}

	function onTagKey(e) {
		if (e.key === 'Enter' || e.key === ',' || e.key === ' ') {
			e.preventDefault();
			addTags(e.target);
			focusTagField();
			return;
		}
		if (e.key === 'Escape') {
			e.target.textContent = '';
			return;
		}
		// Backspace in an empty field takes the tag before it, which is what
		// hands expect of a field made of chips.
		if (e.key === 'Backspace' && !e.target.textContent && tags.length) {
			e.preventDefault();
			dropTag(tags[tags.length - 1]);
			focusTagField();
		}
	}

	function addTags(el) {
		const wanted = tagsIn(el.textContent);
		el.textContent = '';
		const fresh = wanted.filter(function (t) { return tags.indexOf(t) === -1; });
		// Nothing new: leave the chips alone rather than rebuilding them, which
		// would blur the field and land back in here.
		if (!fresh.length) return;
		pushPast(snapshot());
		tags = tags.concat(fresh);
		dirty();
		renderTags();
	}

	// A page carries at least one hashtag, so the last one cannot be taken
	// away: with none there would be nothing to find the page by but its name.
	// The store enforces the same rule for files written by hand.
	function dropTag(tag) {
		if (tags.length <= 1) {
			setState('Ei side må ha minst éin emneknagg', 'is-warn');
			return;
		}
		pushPast(snapshot());
		tags = tags.filter(function (t) { return t !== tag; });
		dirty();
		renderTags();
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

	renderTags();
	focusFirst();
	showState();
	offerDraft();
})();
