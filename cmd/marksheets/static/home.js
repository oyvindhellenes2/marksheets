// The home page's one piece of script: publishing.
//
// It lives here rather than in the editor because publishing is not a per-page
// act and never could be. A commit can be limited to one page; a push sends the
// whole branch, and git offers no way to send part of one — see ADR-0006, which
// is the same limit that gave the pages a repository of their own. The button
// therefore sits where its promise is true.
(function () {
	'use strict';

	const btn = document.getElementById('publish-all');
	if (!btn) return;

	const said = btn.textContent.trim();

	function say(text, cls) {
		btn.textContent = text;
		btn.className = 'btn' + (cls ? ' ' + cls : '');
	}

	function publish() {
		btn.disabled = true;
		say('Publiserer…');
		fetch('/publiser', { method: 'POST' }).then(function (res) {
			if (!res.ok) {
				return res.text().then(function (t) { throw new Error(t.trim() || res.statusText); });
			}
			return res.json();
		}).then(function (info) {
			if (info.published) {
				// The page list carries the unpublished marks, so it is read
				// again rather than patched: the answer is worked out from git
				// on every request and cannot be guessed at from here.
				window.location.reload();
				return;
			}
			say(info.pushError ? 'Commita, men ikkje sendt' : (info.note || 'Lagra i historikk'), 'is-warn');
			if (info.pushError) console.error(info.pushError);
		}).catch(function (err) {
			say('Publisering feila', 'is-warn');
			btn.disabled = false;
			console.error(err);
		});
	}

	btn.addEventListener('click', publish);

	document.addEventListener('keydown', function (e) {
		if (!(e.metaKey || e.ctrlKey) || e.key.toLowerCase() !== 's') return;
		e.preventDefault();
		if (!btn.disabled) publish();
	});

	// Nothing to publish is the resting state, and the button says so.
	if (btn.disabled) say(said);
})();
