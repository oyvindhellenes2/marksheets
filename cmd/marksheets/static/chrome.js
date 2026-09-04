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
	//
	// On a narrow window the same one bit means something else: the index and
	// the page take turns rather than standing side by side, so "open" is a
	// place you are rather than a preference you hold. It therefore starts shut
	// on every load (see <head>) and is not written down — otherwise opening
	// the index on a phone would follow you back to the desk, and every link
	// you followed would land you on the index instead of the page.
	//
	// The width has to agree with the media query in style.css.
	const SIDE_KEY = 'marksheets:sidebar';
	const narrow = window.matchMedia('(max-width: 62rem)');

	// Each sidebar has *two* buttons: one inside it, which goes when it closes,
	// and one in the rail below the header, which appears when it does. Neither
	// is "the" toggle, so they are found by what they do rather than by an id.
	//
	// The share view used to add a third, `#toc-toggle`, in the bar floating
	// over the page. It is gone: that view draws the panel like every other
	// screen does, so the button opened a list already standing open beside it.
	const sideToggles = document.querySelectorAll('[data-toggle="side"]');
	const tocToggles = document.querySelectorAll('[data-toggle="toc"]');

	function announce(buttons, off) {
		buttons.forEach(function (b) { b.setAttribute('aria-expanded', off ? 'false' : 'true'); });
	}

	function saidOff() {
		try { return localStorage.getItem(SIDE_KEY) === '0'; } catch (e) { return false; }
	}

	// Every route to the sidebar goes through here — the button, a swipe, and
	// the breakpoint being crossed — so the class, the stored preference and
	// what the button says about itself cannot drift apart. `remember` is false
	// where the state is a place you are rather than a preference you hold.
	//
	// The event is how anything else hears about it: the tag clamp has to
	// measure a chip, and a chip that is still hidden is zero high.
	function setSide(off, remember) {
		if (root.classList.contains('side-off') === off) return;
		root.classList.toggle('side-off', off);
		if (remember) {
			try { localStorage.setItem(SIDE_KEY, off ? '0' : '1'); } catch (e) { /* private mode */ }
		}
		announce(sideToggles, off);
		// Wide, the two sidebars are columns and both may stand. Narrow, they
		// are views taking turns with the page, so opening one puts the other
		// away. setToc does the same in reverse; neither recurses, because the
		// call it makes is always a *closing* one.
		if (!off && narrow.matches) setToc(true, false);
		document.dispatchEvent(new CustomEvent('marksheets:sidebar'));
	}

	sideToggles.forEach(function (b) {
		b.addEventListener('click', function () {
			setSide(!root.classList.contains('side-off'), !narrow.matches);
		});
	});
	announce(sideToggles, root.classList.contains('side-off'));

	// Crossing the breakpoint — a rotation, a window dragged wider — changes
	// what the bits mean, so they are set again rather than carried across.
	// Going narrow starts both shut, the same as a load does; going wide
	// restores the preferences, which is the only place they were ever kept.
	narrow.addEventListener('change', function (e) {
		setSide(e.matches || saidOff(), false);
		setToc(e.matches || tocSaidOff(), false);
	});

	// ------------------------------------------------------------ swiping
	//
	// Where the index and the page take turns, a thumb expects to pull one in
	// from the side: swipe right for the index, left to go back to the page.
	//
	// Read on touchend and never with preventDefault, so it cannot fight the
	// browser — scrolling, tapping and the caret all behave exactly as they
	// did, and a gesture that turns out to be a scroll simply is not a swipe.
	// The thresholds are what separate a swipe from the two things it sits on
	// top of: a slow, short, or steep drag is somebody scrolling or selecting.
	const SWIPE_MIN = 70;    // px across before it is a swipe at all
	const SWIPE_SLOPE = 2.5; // times further across than down
	const SWIPE_TIME = 500;  // ms — a slower drag is a different gesture

	let began = null;

	// A table or a code block that scrolls sideways owns horizontal gestures
	// inside itself. Taking those would leave it unscrollable on the one kind
	// of screen where it is most likely to be too wide.
	function scrollsSideways(el) {
		for (; el && el !== document.body; el = el.parentElement) {
			if (el.scrollWidth > el.clientWidth + 2) {
				const ox = getComputedStyle(el).overflowX;
				if (ox === 'auto' || ox === 'scroll') return true;
			}
		}
		return false;
	}

	document.addEventListener('touchstart', function (e) {
		began = null;
		// `sideToggles.length` stands in for "there is an index to swipe to at
		// all" — nobody signed in has none. It was a single `toggle` element
		// before the toggles came in pairs; the rename left this reading a name
		// that no longer existed, which threw on every touch rather than
		// failing visibly.
		if (!narrow.matches || !sideToggles.length || e.touches.length !== 1) return;
		// The gutter is the drag handle for moving a line; a horizontal drag
		// starting there is aimed at the line, not at the window.
		if (e.target.closest && e.target.closest('.gutter')) return;
		if (scrollsSideways(e.target)) return;
		const t = e.touches[0];
		began = { x: t.clientX, y: t.clientY, at: Date.now() };
	}, { passive: true });

	document.addEventListener('touchend', function (e) {
		const from = began;
		began = null;
		if (!from || !narrow.matches) return;
		if (Date.now() - from.at > SWIPE_TIME) return;

		const t = e.changedTouches[0];
		const dx = t.clientX - from.x;
		const dy = t.clientY - from.y;
		if (Math.abs(dx) < SWIPE_MIN) return;
		if (Math.abs(dx) < Math.abs(dy) * SWIPE_SLOPE) return;

		// A drag that left text selected was a selection, not a swipe — both
		// the browser's own and the editor's block selection, which is painted
		// by the editor and invisible to getSelection().
		const sel = window.getSelection();
		if (sel && !sel.isCollapsed) return;
		if (document.querySelector('.rows.is-selecting')) return;

		goTo(where() + (dx > 0 ? -1 : 1));
	}, { passive: true });

	// Narrow, the three views sit in a row — index, page, contents — and a
	// swipe is a step along it. Naming the position rather than toggling two
	// independent bits is what keeps "swipe left twice" from ending up
	// somewhere that is neither one thing nor the other.
	function where() {
		if (!root.classList.contains('side-off')) return -1;
		if (!root.classList.contains('toc-off')) return 1;
		return 0;
	}

	function goTo(v) {
		if (v < -1) v = -1;
		if (v > 1) v = 1;
		// A page with no headings has no contents view to step into.
		if (v === 1 && root.classList.contains('toc-none')) v = 0;
		setSide(v !== -1, false);
		setToc(v !== 1, false);
	}

	// ----------------------------------------------------- contents list

	// The headings of the page you are on, read out of the DOM rather than out
	// of the document. That is what lets one list serve both sides of ⌘⏎: the
	// read view emits an id per heading, the editor draws rows, and neither
	// knows about the other. It is also why the list is right *while* a heading
	// is being typed — there is nothing to keep in step, because there is no
	// second copy.
	const TOC_KEY = 'marksheets:toc';
	const tocList = document.getElementById('toc-list');

	function tocSaidOff() {
		try {
			const v = localStorage.getItem(TOC_KEY);
			if (v === '0') return true;
			if (v === '1') return false;
		} catch (e) { /* private mode */ }
		// No opinion yet. Three columns want the room; under 75rem the page
		// comes first. Same rule as the one in <head>.
		return window.matchMedia('(max-width: 75rem)').matches;
	}

	function setToc(off, remember) {
		if (root.classList.contains('toc-off') === off) return;
		root.classList.toggle('toc-off', off);
		if (remember) {
			try { localStorage.setItem(TOC_KEY, off ? '0' : '1'); } catch (e) { /* private mode */ }
		}
		announce(tocToggles, off);
		if (!off && narrow.matches) setSide(true, false);
		document.dispatchEvent(new CustomEvent('marksheets:sidebar'));
	}

	tocToggles.forEach(function (b) {
		b.addEventListener('click', function () {
			setToc(!root.classList.contains('toc-off'), !narrow.matches);
		});
	});
	announce(tocToggles, root.classList.contains('toc-off'));

	// ------------------------------------------------- which list is showing
	//
	// The panel holds two lists — the contents of the page and its history —
	// and shows one at a time. Which one is a class on the panel rather than
	// `hidden` on each: one place to look, and the stylesheet decides what that
	// means, including for the ToC button that should not offer a list a page
	// with no headings does not have.
	//
	// Nothing here fetches. The history arrives through HTMX, and editor.js
	// asks for the switch when it does.
	const panel = document.querySelector('.toc');

	function showHistory(on) {
		if (panel) panel.classList.toggle('showing-history', !!on);
	}

	// Exposed because the editor owns the history button — it is the one that
	// knows whether the list is already open, and closing it is not HTMX's.
	window.marksheetsPanel = { showHistory: showHistory };

	// Whether this screen's panel has controls in it. A page has them; a
	// search, a profile and the type list do not, and neither does the share
	// view. It decides whether a panel with no headings is worth showing at
	// all — with a menu in it, it is.
	if (panel && panel.querySelector('.panel-menu')) root.classList.add('panel-menu-on');

	// Narrow, the panel covers the page and the button that opened it goes with
	// it. Never remembered: shutting the contents to get back to what you were
	// reading is not a statement about how you like the window laid out.
	const tocClose = document.getElementById('toc-close');
	if (tocClose) {
		tocClose.addEventListener('click', function () { setToc(true, false); });
	}

	function headings() {
		const read = document.getElementById('read-view');
		if (read && !read.hidden) {
			// `.ms-h` and not every h1–h6: the read view also carries the page
			// title and the "Lenkjer hit" heading over the backlinks, and
			// neither is a section of the page. Headings that came in through a
			// transclusion are left out for the same reason — they are another
			// page's structure, borrowed, and the editor does not show them at
			// all, so counting them here would make the two lists disagree.
			return Array.prototype.filter.call(read.querySelectorAll('.ms-h'), function (h) {
				return !h.closest('.ms-tx-block');
			}).map(function (h) {
				return { text: h.textContent.trim(), level: +h.tagName.charAt(1), el: h };
			});
		}
		const rows = document.getElementById('rows');
		if (!rows) return [];

		// Direct children only. The editor puts the whole tasks section in a
		// `.tasks-box` of its own, so `> .row-header` is already the page's own
		// headings and leaves out the pinned Oppgåver heading and the Arkiv
		// inside it — the same section the read view omits. Nothing here has to
		// count depths to work that out.
		//
		// A folded heading's children are not rendered at all, so they are not
		// listed either. The folded heading itself still is, which is the level
		// somebody who folded a section is working at; the read view never
		// folds, so ⌘⏎ always has the complete list.
		return Array.prototype.map.call(rows.querySelectorAll(':scope > .row-header'), function (row) {
			const f = row.querySelector('.f-richtext');
			return {
				text: (f ? f.textContent : '').trim(),
				level: +(row.dataset.level || 1),
				el: row
			};
		});
	}

	function buildToc() {
		if (!tocList) return;
		const hs = headings().filter(function (h) { return h.text !== ''; });

		// `toc-none` says this page has no headings to list, without touching
		// `toc-off`, which is the person's preference and not ours to spend. It
		// matters on the first pass: chrome.js may run before the editor has
		// drawn a single row, and closing the panel because it is momentarily
		// empty would quietly shut it on every page load.
		//
		// It hides the panel only where the panel would then hold *nothing* —
		// which, since the page's controls moved in, is no longer true on a
		// page. A brand new page has no headings and still needs Historikk,
		// Les and Del, and losing the toggle along with them left no way to
		// open the panel at all. The stylesheet pairs this with
		// `panel-menu-on`; it also takes `toc-none` into account where it hides
		// the page, so an empty list can never leave a narrow window blank.
		root.classList.toggle('toc-none', hs.length === 0);
		if (!hs.length) {
			tocList.innerHTML = '';
			return;
		}

		// The shallowest heading is the first step, so a page whose sections
		// all start a level down is not drawn permanently indented.
		let top = 6;
		hs.forEach(function (h) { if (h.level < top) top = h.level; });

		const frag = document.createDocumentFragment();
		hs.forEach(function (h, i) {
			const a = document.createElement('a');
			a.className = 'toc-link';
			a.href = '#';
			a.textContent = h.text;
			a.dataset.at = String(i);
			a.dataset.level = String(h.level - top + 1);
			a.style.setProperty('--lvl', String(h.level - top + 1));
			frag.appendChild(a);
		});
		tocList.innerHTML = '';
		tocList.appendChild(frag);
	}

	if (tocList) {
		tocList.addEventListener('click', function (e) {
			const link = e.target.closest('.toc-link');
			if (!link) return;
			e.preventDefault();
			// Resolved again at click time rather than held from build time:
			// the editor replaces every row on a structural edit, so an element
			// kept here would be one that is no longer on the page.
			const target = headings().filter(function (h) { return h.text !== ''; })[+link.dataset.at];
			if (!target) return;

			// Narrow, the contents and the page take turns, so the page has to
			// come back before there is anything to scroll. Two frames: one for
			// the class, one for the layout it causes.
			if (narrow.matches) goTo(0);
			requestAnimationFrame(function () {
				requestAnimationFrame(function () {
					target.el.scrollIntoView({ behavior: 'smooth', block: 'start' });
					target.el.classList.add('is-landed');
					setTimeout(function () { target.el.classList.remove('is-landed'); }, 1300);
				});
			});
		});
	}

	// The editor rewrites its rows on every structural edit and on every
	// keystroke in a heading, and ⌘⏎ swaps which half of the page is on screen.
	// Watching the DOM catches all three without the editor having to know this
	// list exists — it is the same bargain the rest of the app makes about
	// computing an answer rather than being told.
	const shell = document.querySelector('.editor-shell');
	if (shell && tocList) {
		let pending = 0;
		const observer = new MutationObserver(function () {
			clearTimeout(pending);
			pending = setTimeout(buildToc, 200);
		});
		observer.observe(shell, {
			childList: true, subtree: true, characterData: true,
			attributes: true, attributeFilter: ['hidden', 'data-level']
		});
	}
	buildToc();

	// --------------------------------------------------------------- theme

	// Three states in one bit and a bit of absence: light, dark, and neither —
	// the last being "whatever the system says", which is where everybody
	// starts. Only a choice is written down, so somebody who has never pressed
	// the button keeps following their machine when it turns dark in the
	// evening. Read back in <head> before the first paint, like the sidebars.
	const THEME_KEY = 'marksheets:theme';
	const themeBtn = document.getElementById('theme-toggle');
	const systemDark = window.matchMedia('(prefers-color-scheme: dark)');

	function isDark() {
		const chosen = root.getAttribute('data-theme');
		if (chosen === 'dark') return true;
		if (chosen === 'light') return false;
		return systemDark.matches;
	}

	// The button shows what pressing it would give you, not what you have. A
	// moon on a light page is an offer; a moon on a dark page is a description,
	// and descriptions do not belong on buttons.
	function paintTheme() {
		if (!themeBtn) return;
		const dark = isDark();
		themeBtn.textContent = dark ? '☀' : '☾';
		const label = dark ? 'Byt til lys visning' : 'Byt til mørk visning';
		themeBtn.setAttribute('aria-label', label);
		themeBtn.title = label;
	}

	if (themeBtn) {
		themeBtn.addEventListener('click', function () {
			const next = isDark() ? 'light' : 'dark';
			root.setAttribute('data-theme', next);
			try { localStorage.setItem(THEME_KEY, next); } catch (e) { /* private mode */ }
			paintTheme();
		});
		paintTheme();
	}

	// Following the system is a state, not a one-off reading: while nobody has
	// chosen, the machine turning dark at dusk turns this dark with it, and the
	// button has to stop offering what already happened.
	systemDark.addEventListener('change', paintTheme);

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

	// ------------------------------------------------- lining up the search
	//
	// The search box is made to sit exactly over the column the page is
	// written in. It is the one thing in the header you type into, and having
	// it agree with nothing below it read as a bar bolted on above the page
	// rather than as part of it.
	//
	// It has to be measured. The header spans the window while the page is
	// centred in what is left between the two sidebars, so the column moves
	// whenever either is opened or shut — there is no static rule that follows
	// it. `.editor-shell` is that column on a page; `main` is the fallback for
	// the screens that have no editor, and it is the same box one level out.
	//
	// The margin is adjusted by the *difference* rather than set outright,
	// because the box sits in a flex row after the name and does not start at
	// the window's edge. Measure, compare, nudge.
	const searchEl = document.querySelector('.search');

	function alignSearch() {
		if (!searchEl) return;

		// Narrow, the header is a row of its own and the page below it is the
		// full width: there is no column to line up with, and forcing one would
		// squeeze the box that matters most at that width.
		if (narrow.matches) {
			searchEl.style.marginLeft = '';
			searchEl.style.width = '';
			searchEl.style.flex = '';
			return;
		}

		const target = document.querySelector('.editor-shell') || document.querySelector('main');
		if (!target) return;

		// Start from the stylesheet's own position, so this is idempotent and
		// so a resize cannot walk the box sideways one nudge at a time.
		searchEl.style.marginLeft = '';
		searchEl.style.width = '';
		searchEl.style.flex = '';

		const want = target.getBoundingClientRect();
		const have = searchEl.getBoundingClientRect();
		if (!want.width || !have.width) return;

		// Never past whoever is standing to the right — the name and the theme
		// button are not negotiable, and a search box that overlapped them
		// would be worse than one that is a little narrow.
		const room = document.querySelector('.whoami, .whoami-in');
		let width = want.width;
		if (room) {
			const limit = room.getBoundingClientRect().left - want.left - 12;
			if (limit > 0 && width > limit) width = limit;
		}

		// `flex: none` as well as a width: the stylesheet gives the box
		// `flex: 1`, and a flex item that is allowed to grow ignores the width
		// it was given and fills the row anyway. Setting one without the other
		// looks like the measurement was wrong.
		searchEl.style.marginLeft = (parseFloat(getComputedStyle(searchEl).marginLeft) + (want.left - have.left)) + 'px';
		searchEl.style.width = width + 'px';
		searchEl.style.flex = 'none';
	}

	// Every reason the column can move: a sidebar toggled, the window resized,
	// the breakpoint crossed. The editor's own re-renders do not move it.
	document.addEventListener('marksheets:sidebar', alignSearch);
	window.addEventListener('resize', alignSearch);
	narrow.addEventListener('change', alignSearch);
	alignSearch();

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

	// On a narrow window the sidebar starts hidden, and a hidden chip is zero
	// high — measured then, five rows and one row are the same number and the
	// clamp never engages. Opening the index is the first moment there is
	// anything to measure, however it was opened: the button, or a swipe.
	document.addEventListener('marksheets:sidebar', clamp);

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

// ------------------------------------------------------------------ sharing
//
// `Del` puts a link to this page's share view on the clipboard. A button rather
// than a link, because what you want is the address, not to go there yourself.
//
// The share view is behind the same login as everything else. This hands a
// colleague a clean way in to one page; it is not a way in for a stranger.
(function () {
	'use strict';

	// A brief line, then gone. It says what happened and needs no dismissing:
	// the thing it reports is already done, and a notice you have to close is a
	// second task handed to somebody who asked for one.
	//
	// It appears over the button that caused it, which is where the eye already
	// is. Measured from the button rather than parked in a corner of the window,
	// so the answer is next to the question however the bar is laid out.
	function say(text, at) {
		let el = document.getElementById('toast');
		if (!el) {
			el = document.createElement('div');
			el.id = 'toast';
			el.className = 'toast';
			el.setAttribute('role', 'status');
			document.body.appendChild(el);
		}
		el.textContent = text;
		el.classList.remove('is-up');

		if (at) {
			const box = at.getBoundingClientRect();
			el.style.left = (box.left + box.width / 2) + 'px';
			el.style.top = box.top + 'px';
		}

		// Restarting the animation needs a frame with the class off, or a second
		// press inside two seconds shows nothing at all.
		requestAnimationFrame(function () { el.classList.add('is-up'); });
		clearTimeout(el.dataset.timer);
		el.dataset.timer = setTimeout(function () { el.classList.remove('is-up'); }, 2200);
	}

	// The clipboard needs a secure context and a real gesture. Both hold here,
	// but a refusal is still possible — a permission policy, an odd browser —
	// and the fallback has to leave the address somewhere reachable rather than
	// swallowing it.
	function fallback(url) {
		const box = document.createElement('textarea');
		box.value = url;
		box.setAttribute('readonly', '');
		box.style.position = 'fixed';
		box.style.top = '-1000px';
		document.body.appendChild(box);
		box.select();
		let ok = false;
		try { ok = document.execCommand('copy'); } catch (e) { ok = false; }
		document.body.removeChild(box);
		return ok;
	}

	function put(url, btn, text) {
		const done = function () { say(text, btn); };
		if (navigator.clipboard && navigator.clipboard.writeText) {
			navigator.clipboard.writeText(url).then(done, function () {
				if (fallback(url)) done();
				else window.prompt('Kopier lenkja:', url);
			});
			return;
		}
		if (fallback(url)) done();
		else window.prompt('Kopier lenkja:', url);
	}

	document.addEventListener('click', function (e) {
		const btn = e.target.closest('#share-copy');
		if (!btn || btn.disabled) return;

		// The address is the server's to give: it is a token, minted once and
		// handed back on every press after that, so the link somebody was sent
		// last week is the link this copies today.
		btn.disabled = true;
		fetch(btn.dataset.share, { method: 'POST' }).then(function (res) {
			if (!res.ok) return res.text().then(function (t) { throw new Error(t.trim() || res.statusText); });
			return res.json();
		}).then(function (info) {
			// Absolute, and built through URL, so a slug with a Norwegian letter
			// comes out percent-encoded and survives a paste into a chat window.
			//
			// How long it lasts is said here and not left to be discovered: a
			// link that stops working is a thing you want to have been told
			// about while you still had the chance to send another.
			const days = info.days;
			put(new URL(info.url, window.location.origin).href, btn,
				days ? 'Delingslenke kopiert · gjeld i ' + days + ' dagar' : 'Delingslenke kopiert');
		}).catch(function (err) {
			say('Kunne ikkje lage delingslenke', btn);
			console.error(err);
		}).then(function () {
			btn.disabled = false;
		});
	});
})();
