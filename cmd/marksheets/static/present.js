// The presentation: the page you are reading, one slide at a time.
//
// Everything here works on the HTML the read view is already holding. Nothing
// is fetched and nothing is re-rendered, so the article and the slides can
// never disagree about what the page says: the slides *are* the article,
// moved.
//
// Two screens use it. On the share view the article is what the page arrives
// as; in the editor the read view is fetched from the server when somebody
// asks for it, so pressing the button there happens before there is anything
// to cut into slides. That is what `prepare` is for — whoever owns the read
// view puts one there and says when it has arrived. The share view registers
// nothing and the hook is skipped.
//
// This was the second half of del.js, which the share view alone loaded. It is
// its own file now because a page wants it too, and a copy of two hundred
// lines that has to keep agreeing with another copy is exactly the thing this
// app avoids everywhere else.
(function () {
	'use strict';

	const root = document.documentElement;

	// Made on the way in and taken out again on the way out, rather than kept
	// hidden between times. It is `position: fixed` over the whole window, so
	// where it sits in the document does not matter — and building it here
	// means neither template has to carry an empty div waiting for it.
	//
	// It is also why the slides are cut afresh every time: on a page being
	// edited, the article behind them is not the one it was last time.
	let deck = null;

	// The article the slides came out of, hidden while they are up. A screen
	// reader should not be handed the whole page a second time underneath a
	// deck it cannot see.
	let source = null;

	let at = 0;

	function presenting() { return root.classList.contains('presenting'); }

	// The one read article on the screen. `#read-view` is the article itself on
	// the share view and the box HTMX swaps it into on a page, so the class is
	// what is asked for rather than the id.
	function article() { return document.querySelector('.read'); }

	// ------------------------------------------------------------- slides

	// One slide per top-level heading, which is what a page's `h1`s already
	// are: the author divided the page when they wrote it, and a presentation
	// that asked them to do it again with some marker of its own would be a
	// second structure to keep in step with the first.
	//
	// The read view puts a heading's contents in the `.ms-section` right after
	// it, so a slide is a heading and the div that follows. Whatever comes
	// before the first heading belongs with the title.
	function build(from) {
		const title = from.querySelector('.read-title');
		const nodes = Array.prototype.slice.call(from.children);

		const slides = [];
		let opening = [];
		let current = null;

		nodes.forEach(function (el) {
			if (el === title) return;
			// "Lenkjer hit", at the foot of the read view on an ordinary page.
			// It is the wiki talking about itself rather than a part of the
			// page, and it would otherwise be swept onto whatever slide came
			// last — the share view never had to think about this, because a
			// shared page is drawn without backlinks in the first place.
			if (el.classList.contains('backlinks')) return;
			if (el.classList.contains('ms-h1')) {
				current = { heading: el.textContent.trim(), body: [] };
				slides.push(current);
				return;
			}
			// The section belonging to the heading just seen.
			if (current && el.classList.contains('ms-section')) {
				current.body.push(el);
				return;
			}
			// A stray line at the top level after a heading's section — or,
			// before any heading at all, the page's opening words.
			(current ? current.body : opening).push(el);
		});

		// The title slide. It exists even when the page opens straight into a
		// heading, because a presentation that starts mid-topic gives a room no
		// idea what it is looking at.
		deck.appendChild(slide(title ? title.textContent.trim() : '', opening, true));
		slides.forEach(function (s) { deck.appendChild(slide(s.heading, s.body, false)); });
	}

	function slide(heading, body, isTitle) {
		const el = document.createElement('section');
		el.className = 'slide' + (isTitle ? ' is-title' : '');

		if (heading) {
			const h = document.createElement('h2');
			h.className = 'slide-title';
			h.textContent = heading;
			el.appendChild(h);
		}
		const inner = document.createElement('div');
		inner.className = 'slide-body';
		// Clones, so leaving the presentation leaves the article as it was —
		// and so the links stay struck out, since they were closed before this.
		//
		// A heading's whole section arrives as one `.ms-section` div, and it is
		// unwrapped rather than dropped in whole. Two columns can only break
		// between children, so a body holding a single child is a body that
		// cannot be split: it would fill the first column, overflow, and leave
		// the second empty. Unwrapped, the paragraphs flow.
		body.forEach(function (b) {
			if (b.classList.contains('ms-section')) {
				// Copied out of the live NodeList first: appending a node removes
				// it from the list being walked, and a live list walked while it
				// shrinks drops every other child.
				const kids = Array.prototype.slice.call(b.cloneNode(true).childNodes);
				kids.forEach(function (kid) { inner.appendChild(kid); });
				return;
			}
			inner.appendChild(b.cloneNode(true));
		});
		el.appendChild(inner);
		return el;
	}

	// ------------------------------------------------------------ two columns

	// Two columns, but only where one would not have fitted. A slide holding
	// three lines split into two half-empty columns looks like a mistake; a
	// slide holding thirty and running off the bottom is one. So the question is
	// asked of the layout rather than answered in advance — measure, and go to
	// two columns only when the content overflows the screen it has.
	//
	// Measured with the columns off, or the second measurement would be taken of
	// a layout the first one caused.
	function reflow(el) {
		const body = el.querySelector('.slide-body');
		if (!body) return;
		body.classList.remove('is-two-col', 'is-scroll');
		if (body.scrollHeight <= body.clientHeight + 4) return; // one column did

		body.classList.add('is-two-col');
		// Two columns of a fixed height do not stop at two: what does not fit
		// flows into a third, off the right-hand edge, where it is clipped. If
		// that has happened the slide is simply too full for the screen, and one
		// column that scrolls is the honest answer — losing the end of somebody's
		// paragraph without saying so is the one outcome worth ruling out.
		if (body.scrollWidth > body.clientWidth + 4) {
			body.classList.remove('is-two-col');
			body.classList.add('is-scroll');
		}
	}

	// -------------------------------------------------------------- showing

	// `.slide` and not every child: the counter and the way out live in the
	// deck too, and counting them as slides would add blank ones at the end.
	function slidesNow() {
		return deck ? Array.prototype.slice.call(deck.querySelectorAll('.slide')) : [];
	}

	function show(i) {
		const all = slidesNow();
		if (!all.length) return;
		at = Math.max(0, Math.min(i, all.length - 1));
		all.forEach(function (el, n) {
			const on = n === at;
			el.classList.toggle('is-on', on);
			// Hidden rather than merely unpainted: a screen reader should not
			// walk twenty slides to reach the one on screen.
			el.hidden = !on;
		});
		reflow(all[at]);
		deck.dataset.at = String(at + 1);
		deck.dataset.of = String(all.length);
		const count = deck.querySelector('.slide-count');
		if (count) count.textContent = (at + 1) + ' / ' + all.length;
	}

	// --------------------------------------------------------- in and out

	// The way out, inside the deck rather than left to the button that opened
	// it. The deck covers the window, and on a page the button is in the
	// right-hand panel, which the deck is over the top of — so Escape would
	// have been the only way back, and a key nobody was told about is not a
	// way back at all.
	function exitButton() {
		const b = document.createElement('button');
		b.type = 'button';
		b.className = 'slide-exit';
		b.setAttribute('aria-label', 'Avslutt presentasjonen');
		b.title = 'Avslutt presentasjonen (Esc)';
		b.textContent = '×';
		b.addEventListener('click', function () { close(); });
		return b;
	}

	function open() {
		if (presenting()) return;
		const from = article();
		// Nothing to show. It is not an error — a page whose read view has not
		// arrived is a press that came too early — and it leaves the screen as
		// it was rather than putting up an empty deck.
		if (!from) return;

		deck = document.createElement('div');
		deck.className = 'slides';
		build(from);

		const count = document.createElement('div');
		count.className = 'slide-count';
		deck.appendChild(count);
		deck.appendChild(exitButton());
		document.body.appendChild(deck);

		source = from;
		source.hidden = true;
		root.classList.add('presenting');
		// Always from the beginning. Opening the presentation is starting it.
		show(0);
	}

	function close() {
		if (!presenting()) return;
		root.classList.remove('presenting');
		if (deck) deck.remove();
		deck = null;
		if (source) source.hidden = false;
		source = null;
	}

	// Arrow keys move between slides; Escape leaves. Only while presenting, so
	// reading and writing are left alone — and captured, so that the keys the
	// presentation does take never reach the editor underneath, which has its
	// own uses for Escape and for the arrows.
	document.addEventListener('keydown', function (e) {
		if (!presenting()) return;
		if (e.metaKey || e.ctrlKey || e.altKey) return;
		switch (e.key) {
			case 'ArrowRight':
			case 'PageDown':
			case ' ':
				show(at + 1);
				break;
			case 'ArrowLeft':
			case 'PageUp':
				show(at - 1);
				break;
			case 'Home':
				show(0);
				break;
			case 'End':
				show(slidesNow().length - 1);
				break;
			case 'Escape':
				close();
				break;
			default:
				return;
		}
		e.preventDefault();
		e.stopPropagation();
	}, true);

	// A slide that fitted in a tall window may not fit in a short one.
	window.addEventListener('resize', function () {
		if (!presenting()) return;
		const all = slidesNow();
		if (all[at]) reflow(all[at]);
	});

	// Jumping to a heading from the contents list is a reading gesture, so it
	// takes you out of the presentation to the place it points at. Without this
	// the click would scroll an article nobody can see.
	document.addEventListener('click', function (e) {
		if (presenting() && e.target.closest('.toc-link')) close();
	});

	// One handler for every button that opens a presentation, found by what it
	// does rather than by an id — the share view's floats over the page and the
	// editor's is a word in the panel menu, and neither is "the" button.
	document.addEventListener('click', function (e) {
		const btn = e.target.closest('[data-present]');
		if (!btn || btn.disabled) return;
		const ready = window.marksheetsPresent.prepare;
		Promise.resolve(ready ? ready() : null).then(open, function (err) {
			console.error(err);
		});
	});

	window.marksheetsPresent = {
		open: open,
		close: close,
		presenting: presenting,
		// Set by whoever owns the read view — the editor does, because there
		// the article is fetched rather than sent with the page. It returns
		// something thenable and the presentation opens once it settles. Left
		// null, the article is expected to be on the screen already.
		prepare: null,
	};
})();
