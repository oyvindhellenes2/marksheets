// The share view: one page, read, with nowhere else to go — and a presentation
// built out of the same article.
//
// Everything here works on the HTML the server already sent. Nothing is fetched
// and nothing is re-rendered, so the reading view and the slides can never
// disagree about what the page says: the slides *are* the article, moved.
(function () {
	'use strict';

	const share = document.querySelector('.share');
	if (!share) return;

	const read = document.getElementById('read-view');
	const deck = document.getElementById('slides');
	const button = document.getElementById('mode-present');

	// ------------------------------------------------------------- no exits

	// A shared page is one page. The links that lead further into the wiki are
	// struck out rather than removed: the words they were on are part of the
	// sentence, and deleting them would edit somebody's writing to make a rule
	// true. Only inward links go — an ordinary `[text](url)` to somewhere else
	// on the internet is a reference the author made on purpose, and an
	// attachment is part of this page rather than a way off it.
	const INWARD = 'a.ms-link, a.ms-tx-source, a.ms-owner, a.ms-task-open';

	function closeExits(within) {
		within.querySelectorAll(INWARD).forEach(function (a) {
			const dead = document.createElement('span');
			dead.className = a.className + ' is-dead';
			dead.title = 'Lenkja er av på ei delt side';
			dead.innerHTML = a.innerHTML;
			a.replaceWith(dead);
		});
	}
	closeExits(read);

	// ------------------------------------------------------------- slides

	// One slide per top-level heading, which is what a page's `h1`s already are:
	// the author divided the page when they wrote it, and a presentation that
	// asked them to do it again with some marker of its own would be a second
	// structure to keep in step with the first.
	//
	// The read view puts a heading's contents in the `.ms-section` right after
	// it, so a slide is a heading and the div that follows. Whatever comes
	// before the first heading belongs with the title.
	function build() {
		if (deck.dataset.built) return;
		deck.dataset.built = '1';

		const title = read.querySelector('.read-title');
		const nodes = Array.prototype.slice.call(read.children);

		const slides = [];
		let opening = [];
		let current = null;

		nodes.forEach(function (el) {
			if (el === title) return;
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
			// A stray line at the top level after a heading's section — or, before
			// any heading at all, the page's opening words.
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

	let at = 0;

	// `.slide` and not every child: the counter lives in the deck too, and
	// counting it as a slide would add a blank one at the end.
	function slidesNow() { return Array.prototype.slice.call(deck.querySelectorAll('.slide')); }

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
		count.textContent = (at + 1) + ' / ' + all.length;
	}

	const count = document.createElement('div');
	count.className = 'slide-count';

	function presenting() { return share.classList.contains('is-presenting'); }

	function present(on) {
		if (on) build();
		share.classList.toggle('is-presenting', on);
		// On the root as well: the contents list is a sibling of the article's
		// ancestor, not a descendant of this section, so nothing inside `.share`
		// can select it. A presentation wants the whole window.
		document.documentElement.classList.toggle('presenting', on);
		read.hidden = on;
		deck.hidden = !on;
		button.textContent = on ? 'Lesevising' : 'Presentasjonsvisning';
		if (on) {
			if (!count.parentNode) deck.appendChild(count);
			show(at);
		}
	}

	button.addEventListener('click', function () { present(!presenting()); });

	// Arrow keys move between slides; Escape leaves. Only while presenting, so
	// reading is left alone — and the handler stands down completely rather than
	// swallowing keys it has no use for.
	document.addEventListener('keydown', function (e) {
		if (!presenting()) return;
		if (e.metaKey || e.ctrlKey || e.altKey) return;
		switch (e.key) {
			case 'ArrowRight':
			case 'PageDown':
			case ' ':
				e.preventDefault();
				show(at + 1);
				break;
			case 'ArrowLeft':
			case 'PageUp':
				e.preventDefault();
				show(at - 1);
				break;
			case 'Home':
				e.preventDefault();
				show(0);
				break;
			case 'End':
				e.preventDefault();
				show(slidesNow().length - 1);
				break;
			case 'Escape':
				e.preventDefault();
				present(false);
				break;
		}
	});

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
		if (presenting() && e.target.closest('.toc-link')) present(false);
	});
})();
