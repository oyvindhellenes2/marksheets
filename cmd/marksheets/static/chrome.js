// The chrome around every page: the sidebar toggle and the search box.
//
// Small enough to be vanilla, and it has to be, because it runs on the editor
// too — where HTMX is deliberately not in charge of anything (see SPEC,
// "Implementation notes"). What is fetched here is still HTMX's: the sidebar's
// index and the list under the search box are both server HTML.
(function () {
	'use strict';

	const root = document.documentElement;

	// ------------------------------------------------------------- sidebar

	// Which way the sidebar is left is a view preference, not content, so it
	// lives in localStorage like the folded headings do — and it is read again
	// in <head>, before the first paint, so the page never opens the sidebar
	// only to shut it in front of you.
	const SIDE_KEY = 'marksheets:sidebar';
	const toggle = document.getElementById('side-toggle');

	if (toggle) {
		toggle.addEventListener('click', function () {
			const off = root.classList.toggle('side-off');
			try { localStorage.setItem(SIDE_KEY, off ? '0' : '1'); } catch (e) { /* private mode */ }
			toggle.setAttribute('aria-expanded', off ? 'false' : 'true');
		});
		toggle.setAttribute('aria-expanded', root.classList.contains('side-off') ? 'false' : 'true');
	}

	// -------------------------------------------------------------- search

	const box = document.getElementById('search-input');
	const menu = document.getElementById('search-menu');
	if (!box || !menu) return;

	function close() { menu.innerHTML = ''; }

	// ↑/↓ walk the suggestions and Enter takes the one you are on; with none
	// picked, Enter submits the form and searches everything. The menu is
	// server HTML swapped in under us, so the current item is found in the DOM
	// each time rather than remembered in a variable that would go stale.
	function items() { return Array.prototype.slice.call(menu.querySelectorAll('.search-item')); }

	function move(by) {
		const list = items();
		if (!list.length) return false;
		const at = list.findIndex(function (el) { return el.classList.contains('is-current'); });
		const next = at === -1
			? (by > 0 ? 0 : list.length - 1)
			: (at + by + list.length) % list.length;
		list.forEach(function (el) { el.classList.remove('is-current'); });
		list[next].classList.add('is-current');
		return true;
	}

	box.addEventListener('keydown', function (e) {
		if (e.key === 'Escape') { close(); box.blur(); return; }
		if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
			if (move(e.key === 'ArrowDown' ? 1 : -1)) e.preventDefault();
			return;
		}
		if (e.key === 'Enter') {
			const current = menu.querySelector('.search-item.is-current');
			if (current) {
				e.preventDefault();
				window.location.href = current.href;
			}
			// Otherwise the form submits and the whole scan runs.
		}
	});

	// A menu left open over the page after you have looked away is in the way.
	document.addEventListener('click', function (e) {
		if (!e.target.closest('.search')) close();
	});
	box.addEventListener('blur', function () {
		// Late enough for a click on an item to land first.
		setTimeout(close, 150);
	});

	// ⌘K / Ctrl-K puts the caret in the box from anywhere, including from
	// inside the editor — where every other key belongs to the document.
	document.addEventListener('keydown', function (e) {
		if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
			e.preventDefault();
			box.focus();
			box.select();
		}
	});
})();

// ------------------------------------------------------------------- tags
//
// A long tag list pushes the pages off the bottom of the sidebar, so it is cut
// at five rows with a way to see the rest.
//
// Five *rows*, not five tags: how many fit on a line depends on how long the
// words are, which only the browser knows. So the clamp is measured from a real
// chip rather than written into the stylesheet, and the button appears only
// when there is actually something hidden behind it.
(function () {
	'use strict';

	const ROWS = 5;
	let open = false;

	function clamp() {
		const tags = document.querySelector('.side-tags');
		const more = document.querySelector('.tag-more');
		if (!tags || !more) return;

		const chip = tags.querySelector('.tag-link');
		if (!chip) return;
		const gap = parseFloat(getComputedStyle(tags).rowGap) || 0;
		const limit = ROWS * chip.offsetHeight + (ROWS - 1) * gap;

		tags.style.maxHeight = '';
		const full = tags.scrollHeight;
		if (full <= limit + 1) {
			more.hidden = true;
			return;
		}
		more.hidden = false;
		more.textContent = open ? 'Vis mindre' : 'Vis meir';
		tags.style.maxHeight = open ? '' : limit + 'px';
		tags.classList.toggle('is-clamped', !open);
	}

	// The list is swapped in by HTMX when a tag is picked, so the button is
	// found at click time rather than bound to one that may since have gone.
	document.addEventListener('click', function (e) {
		if (!e.target.closest('.tag-more')) return;
		open = !open;
		clamp();
	});

	// Filtering the list replaces the tags too, and the new set is a different
	// height. Opened once, it stays open — closing it under you would look
	// like the click did something else.
	document.body.addEventListener('htmx:afterSwap', function (e) {
		if (e.target.closest && e.target.closest('#side-tags')) clamp();
	});
	window.addEventListener('resize', clamp);
	clamp();
})();

// --------------------------------------------------------------- publishing
//
// Publishing is not a per-page act and never could be: a commit can be limited
// to one page, but a push sends the whole branch and git offers no way to send
// part of one ([ADR-0006]). It sat on the index page for that reason
// ([ADR-0014]); with the index page gone it sits in the sidebar, which is the
// same argument — chrome rather than a page — and is why the button may now be
// pressed from anywhere ([ADR-0019]).
//
// `⌘S` is deliberately *not* bound while the editor is on screen. There, hands
// press it meaning "save my typing", which already happened on a timer; making
// that push to everybody would be the most expensive misunderstanding in the
// app.
(function () {
	'use strict';

	function button() { return document.getElementById('publish-all'); }

	function say(text, cls) {
		const btn = button();
		if (!btn) return;
		btn.textContent = text;
		btn.className = 'btn side-publish' + (cls ? ' ' + cls : '');
		btn.style.whiteSpace = 'normal';
	}

	function publish() {
		const btn = button();
		if (!btn || btn.disabled) return;
		btn.disabled = true;
		say('Publiserer…');
		fetch('/publiser', { method: 'POST' }).then(function (res) {
			if (!res.ok) {
				return res.text().then(function (t) { throw new Error(t.trim() || res.statusText); });
			}
			return res.json();
		}).then(function (info) {
			if (info.published) {
				// What is unpublished is worked out from git on every request
				// and cannot be guessed at from here, so the page is read
				// again rather than patched.
				window.location.reload();
				return;
			}
			say(info.pushError ? 'Commita, men ikkje sendt' : (info.note || 'Lagra i historikk'), 'is-warn');
			if (info.pushError) console.error(info.pushError);
		}).catch(function (err) {
			say('Publisering feila', 'is-warn');
			const btn = button();
			if (btn) btn.disabled = false;
			console.error(err);
		});
	}

	document.addEventListener('click', function (e) {
		if (e.target.closest('#publish-all')) publish();
	});

	document.addEventListener('keydown', function (e) {
		if (!(e.metaKey || e.ctrlKey) || e.key.toLowerCase() !== 's') return;
		if (document.querySelector('.editor-shell')) return; // see above
		e.preventDefault();
		publish();
	});
})();
