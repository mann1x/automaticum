// run_game: does space_invaders.html actually run, and does it play by its rules?
//
//   ./run_game <file.html> [frames]
//
// A page that parses is not a page that works, so this answers by running it:
// a stub DOM and canvas, every <script> block executed in one context, then
// animation frames pumped with a fake clock (16.7ms a frame). The page opens in
// a 3-second countdown, so the play path first runs around frame 180; the
// default of 400 frames goes well past it. `player.frameCount` only moves on
// the play path, which makes it the witness that the game is playing rather
// than merely loading.
//
// Once the page plays, each rule below loads it into a fresh context (Math.random
// seeded), puts the game into one situation, calls update() and compares what
// happened with how the game is meant to behave: the cannon's speed and bounds,
// one shot at a time with a cooldown, the formation's march, step-down and
// speed-up, shots against aliens and shields, bombs against shields and the
// cannon, lives, game over, the next wave. Rules judge behaviour, not text.
//
// Prints ONE JSON line:
//   {"ok":..,"error":..,"frames_run":..,"started":..,"state":..,"player_frames":..,
//    "reached_playing":..,"game_logic":{"ok":..,"passed":..,"total":..,"failed":[..],"warnings":[..]}}
// `ok` means the page loads and plays; game_logic is judged only when it does.

const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const file = process.argv[2] || "space_invaders.html";
const FRAMES = Number(process.argv[3] || 400);
const SHOWN = path.basename(file);

// ---------------------------------------------------------------------------
// The stub browser
// ---------------------------------------------------------------------------

function context2d() {
	const fixed = { canvas: { width: 640, height: 480 }, globalAlpha: 1, lineWidth: 1, shadowBlur: 0 };
	return new Proxy(fixed, {
		get(target, prop) {
			if (prop in target) return target[prop];
			if (typeof prop === "symbol") return undefined;
			return (...args) => {
				if (prop === "measureText") return { width: (String(args[0] ?? "").length || 1) * 7 };
				if (prop === "createLinearGradient" || prop === "createRadialGradient" || prop === "createPattern") {
					return { addColorStop() {} };
				}
				if (prop === "getImageData" || prop === "createImageData") {
					return { data: new Uint8ClampedArray(4), width: 1, height: 1 };
				}
				return undefined;
			};
		},
		set(target, prop, value) {
			target[prop] = value;
			return true;
		},
	});
}

function makeElement(id) {
	const el = {
		id,
		tagName: String(id).toUpperCase(),
		width: 640,
		height: 480,
		style: {},
		dataset: {},
		children: [],
		textContent: "",
		innerHTML: "",
		className: "",
		classList: { add() {}, remove() {}, toggle() {}, contains: () => false },
		getContext: () => context2d(),
		addEventListener() {},
		removeEventListener() {},
		appendChild(child) {
			el.children.push(child);
			return child;
		},
		removeChild() {},
		setAttribute() {},
		getAttribute: () => null,
		focus() {},
		blur() {},
		getBoundingClientRect: () => ({ left: 0, top: 0, width: 640, height: 480, right: 640, bottom: 480 }),
	};
	return el;
}

function seeded(seed) {
	let a = seed >>> 0;
	return () => {
		a = (a + 0x6d2b79f5) >>> 0;
		let t = a;
		t = Math.imul(t ^ (t >>> 15), t | 1);
		t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
		return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
	};
}

// One page context. `clock.now` is what performance.now() returns; scheduled
// callbacks queue in `__frames` and are run by __runDue(), inside the context,
// so a page that loops forever is cut off by the vm timeout.
function makePage(clock, random) {
	const elements = new Map();
	const listeners = [];
	const byId = (id) => {
		if (!elements.has(id)) elements.set(id, makeElement(id));
		return elements.get(id);
	};
	const on = (type, fn) => {
		if (typeof fn === "function") listeners.push([type, fn]);
	};
	const frames = [];
	const audioNode = () =>
		new Proxy(
			{},
			{
				get(target, prop) {
					if (prop in target) return target[prop];
					if (prop === "frequency" || prop === "gain" || prop === "detune") {
						return { value: 0, setValueAtTime() {}, linearRampToValueAtTime() {}, exponentialRampToValueAtTime() {} };
					}
					if (typeof prop === "symbol") return undefined;
					return () => audioNode();
				},
				set(target, prop, value) {
					target[prop] = value;
					return true;
				},
			},
		);
	const Audio = function () {
		return {
			currentTime: 0,
			state: "running",
			destination: audioNode(),
			createOscillator: () => audioNode(),
			createGain: () => audioNode(),
			createBiquadFilter: () => audioNode(),
			createBufferSource: () => audioNode(),
			createBuffer: () => ({ getChannelData: () => new Float32Array(64) }),
			resume: () => Promise.resolve(),
			close: () => Promise.resolve(),
			play: () => Promise.resolve(),
			pause() {},
		};
	};
	const document = {
		getElementById: byId,
		querySelector: (sel) => byId(String(sel).replace(/^[#.]/, "")),
		querySelectorAll: () => [],
		createElement: (tag) => makeElement(tag),
		addEventListener: on,
		removeEventListener() {},
		body: makeElement("body"),
		documentElement: makeElement("html"),
		hidden: false,
		visibilityState: "visible",
	};
	let math = Math;
	if (random) {
		math = Object.create(Math);
		math.random = random;
	}
	const page = {
		document,
		console: { log() {}, warn() {}, error() {}, info() {}, debug() {} },
		requestAnimationFrame: (cb) => {
			frames.push(cb);
			return frames.length;
		},
		cancelAnimationFrame() {},
		setTimeout: (cb) => {
			if (typeof cb === "function") frames.push(cb);
			return 0;
		},
		clearTimeout() {},
		setInterval: () => 0,
		clearInterval() {},
		addEventListener: on,
		removeEventListener() {},
		performance: { now: () => clock.now },
		Date,
		Math: math,
		JSON,
		AudioContext: Audio,
		webkitAudioContext: Audio,
		Audio,
		Image: () => makeElement("img"),
		alert() {},
		innerWidth: 1024,
		innerHeight: 768,
		devicePixelRatio: 1,
		localStorage: { getItem: () => null, setItem() {}, removeItem() {} },
		__listeners: listeners,
		__frames: frames,
		__clock: clock,
	};
	page.window = page;
	page.self = page;
	page.globalThis = page;
	vm.createContext(page);
	vm.runInContext(
		"function __runDue(t) { const due = __frames.splice(0, __frames.length); for (const cb of due) cb(t); return due.length; }",
		page,
	);
	return page;
}

// The script blocks, each with the line of the file it starts on.
function scriptBlocks(html) {
	const out = [];
	for (const m of html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/gi)) {
		if (/\bsrc\s*=/.test(m[1]) || !m[2].trim()) continue;
		const start = m.index + m[0].indexOf(">") + 1;
		const line = html.slice(0, start).split("\n").length - 1;
		out.push({ code: m[2], line });
	}
	return out;
}

// "Name: message (file:line)", the line being the file's, not the block's.
function describe(error) {
	const name = (error && error.name) || "Error";
	const message = (error && error.message) || String(error);
	const stack = String((error && error.stack) || "");
	const esc = SHOWN.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
	const at = stack.match(new RegExp(`${esc}:(\\d+)`));
	return at ? `${name}: ${message} (${SHOWN}:${at[1]})` : `${name}: ${message}`;
}

function loadPage(blocks, clock, random) {
	const page = makePage(clock, random);
	for (const b of blocks) {
		vm.runInContext(b.code, page, { filename: SHOWN, lineOffset: b.line, timeout: 10000 });
	}
	return page;
}

function verdict(fields) {
	return {
		ok: false,
		error: null,
		frames_run: 0,
		started: false,
		state: null,
		player_frames: 0,
		reached_playing: false,
		...fields,
	};
}

// ---------------------------------------------------------------------------
// Load check: parse, run, pump frames, reach PLAYING
// ---------------------------------------------------------------------------

function run(html, blocks) {
	if (blocks.length === 0) {
		const opened = (html.match(/<script\b[^>]*>/gi) || []).length;
		const closed = (html.match(/<\/script>/gi) || []).length;
		return verdict({
			error: opened > closed ? `unclosed <script> (${opened} opened, ${closed} closed)` : "no script blocks",
		});
	}
	let framesRun = 0;
	const clock = {
		get now() {
			return framesRun * 16.7;
		},
	};
	let page;
	try {
		page = loadPage(blocks, clock, null);
	} catch (error) {
		return verdict({ error: describe(error) });
	}
	let started = false;
	try {
		if (typeof page.startGame === "function" && page.__frames.length === 0) {
			page.startGame();
			started = true;
		}
	} catch (error) {
		return verdict({ error: `startGame: ${describe(error)}` });
	}
	try {
		while (framesRun < FRAMES) {
			const n = vm.runInContext(`__runDue(${framesRun * 16.7})`, page, { timeout: 5000 });
			if (n === 0) break;
			framesRun += 1;
		}
	} catch (error) {
		return verdict({ error: `frame ${framesRun}: ${describe(error)}`, frames_run: framesRun, started, player_frames: null });
	}
	if (framesRun === 0) {
		return verdict({ error: "no animation frame was ever scheduled", started });
	}
	const probe = (expr) => {
		try {
			return vm.runInContext(expr, page, { timeout: 2000 });
		} catch {
			return null;
		}
	};
	const state = probe("typeof gameState === 'string' ? gameState : null");
	const playerFrames = probe(
		"(typeof player === 'object' && player && typeof player.frameCount === 'number') ? player.frameCount : null",
	);
	if (!(playerFrames !== null && playerFrames > 0)) {
		return verdict({
			error:
				state === null
					? `the game never played: player.frameCount unreadable after ${framesRun} frames`
					: `the game never played: still in ${state} after ${framesRun} frames (the countdown ends near frame 180)`,
			frames_run: framesRun,
			started,
			state,
			player_frames: playerFrames,
		});
	}
	return verdict({ ok: true, frames_run: framesRun, started, state, player_frames: playerFrames, reached_playing: true });
}

// ---------------------------------------------------------------------------
// game_logic: the rules
// ---------------------------------------------------------------------------

const HELPERS = `
const __warnings = [];
function __warn(text) { __warnings.push(String(text)); }
const __W = (typeof WIDTH === 'number') ? WIDTH : 640;
const __H = (typeof HEIGHT === 'number') ? HEIGHT : 480;
const __keyInfo = { ArrowLeft: ['ArrowLeft', 37], ArrowRight: ['ArrowRight', 39], Space: [' ', 32], Enter: ['Enter', 13] };
function __key(code, down) {
	const [key, kc] = __keyInfo[code];
	const ev = { type: down ? 'keydown' : 'keyup', key, code, keyCode: kc, which: kc, repeat: false,
		preventDefault() {}, stopPropagation() {} };
	for (const [type, fn] of __listeners) if (type === ev.type) fn(ev);
}
function __release() { for (const k of ['ArrowLeft', 'ArrowRight', 'Space']) __key(k, false); }
function __step(n) { for (let i = 0; i < (n || 1); i++) update(); }
function __tick(n) { for (let i = 0; i < (n || 1); i++) { __clock.now += 16.7; update(); } }
function __alive() { return aliens.filter((a) => a.alive); }
function __text(id) {
	const e = document.getElementById(id);
	return String(e.innerText !== undefined && e.innerText !== '' ? e.innerText : e.textContent).trim();
}
// Wave 1, mid-play, nothing in the air, no bombs due.
function __arena() {
	__release();
	level = 1;
	setupWave(1);
	gameState = 'PLAYING';
	transitionTimer = 0;
	playerShot = null;
	bombs = [];
	player.x = 300; player.lives = 3; player.score = 0; player.cooldown = 0; player.hitTimer = 0;
	fleet.bombTimer = 1e9;
}
function __snap() { return aliens.map((a) => [a.x, a.y]); }
// Step until the formation moves; report how far each alien went and after how many frames.
function __moveUntil(max) {
	const before = __snap();
	for (let n = 1; n <= max; n++) {
		__step();
		if (aliens.some((a, i) => a.x !== before[i][0] || a.y !== before[i][1])) {
			const live = aliens.map((a, i) => [a, i]).filter(([a]) => a.alive);
			return { n, dx: live.map(([a, i]) => a.x - before[i][0]), dy: live.map(([a, i]) => a.y - before[i][1]) };
		}
	}
	return null;
}
function __same(list) { return list.every((v) => v === list[0]); }
function __shoot(a) { playerShot = { x: a.x + a.w / 2 - 1, y: a.y + a.h - 2, w: 2, h: 10, vy: -8 }; }
function __bombAt(x, y) { const b = { x, y, w: 3, h: 8, vy: 3 }; bombs.push(b); return b; }
function __shieldHp() { return shieldBlocks.reduce((s, b) => s + Math.max(0, b.hp), 0); }
function __rows() { return [...new Set(aliens.map((a) => a.y))].sort((p, q) => p - q); }
`;

const RULES = {
	wave_setup: `setupWave(1);
		if (!Array.isArray(aliens) || aliens.length !== 55) return 'a new wave has ' + (aliens && aliens.length) + ' aliens, expected 55 (5 rows of 11)';
		if (!aliens.every((a) => a.alive)) return 'a new wave starts with dead aliens';
		const rows = __rows();
		if (rows.length !== 5) return 'a new wave has ' + rows.length + ' rows, expected 5';
		for (const y of rows) if (aliens.filter((a) => a.y === y).length !== 11) return 'a row of the new wave does not hold 11 aliens';
		for (const a of aliens) {
			if (a.x < 0 || a.x + a.w > __W || a.y < 0) return 'an alien starts off the screen at (' + a.x + ', ' + a.y + ')';
			for (const b of aliens) if (a !== b && a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y) return 'two aliens of a new wave overlap';
		}
		if (!Array.isArray(shieldBlocks) || __shieldHp() <= 0) return 'a new wave has no shields';
		const xs = [...new Set(shieldBlocks.map((b) => b.x))].sort((p, q) => p - q);
		let groups = 1; for (let i = 1; i < xs.length; i++) if (xs[i] - xs[i - 1] > 20) groups++;
		if (groups !== 4) return 'there are ' + groups + ' shields, expected 4';
		const low = Math.max(...aliens.map((a) => a.y + a.h));
		if (!shieldBlocks.every((b) => b.y > low && b.y + b.h < player.y)) return 'the shields are not between the aliens and the cannon';
		return '';`,

	countdown: `if (gameState !== 'COUNTDOWN') return 'the page opened in state ' + gameState + ', expected COUNTDOWN';
		const x0 = player.x, f = player.frameCount; __key('ArrowRight', true);
		let n = 0; while (gameState === 'COUNTDOWN' && n < 900) { __tick(); n++; }
		if (gameState !== 'PLAYING') return 'fifteen seconds after loading the state is ' + gameState + ', expected PLAYING';
		if (n < 120) return 'the countdown ended after ' + (n / 60).toFixed(1) + ' s, expected about 3';
		if (n > 240) return 'the countdown took ' + (n / 60).toFixed(1) + ' s, expected about 3';
		if (player.x !== x0 || player.frameCount !== f) return 'the cannon moved during the countdown';
		__tick();
		if (player.frameCount !== f + 1) return 'the game did not run on the frame after the countdown';
		if (player.x === x0) return 'holding right did not move the cannon once play began';
		return '';`,

	move: `__arena(); const x0 = player.x, y0 = player.y; __key('ArrowRight', true); __step(10);
		let d = player.x - x0;
		if (!(d >= 36 && d <= 44)) return 'holding right for 10 frames moved the cannon ' + d + 'px, expected 40 (4 a frame)';
		__key('ArrowRight', false); const x1 = player.x; __step(5);
		if (player.x !== x1) return 'the cannon kept moving after right was released';
		__key('ArrowLeft', true); __step(10); d = x1 - player.x;
		if (!(d >= 36 && d <= 44)) return 'holding left for 10 frames moved the cannon ' + d + 'px, expected 40';
		if (player.y !== y0) return 'moving changed the height of the cannon';
		return '';`,

	bounds: `__arena(); player.x = 6; __key('ArrowLeft', true); __step(10);
		if (player.x !== 0) return 'holding left at the left edge leaves the cannon at x ' + player.x + ', expected 0';
		__key('ArrowLeft', false); player.x = __W - player.w - 6; __key('ArrowRight', true); __step(10);
		if (player.x !== __W - player.w) return 'holding right at the right edge leaves the cannon at x ' + player.x + ', expected ' + (__W - player.w);
		return '';`,

	fire: `__arena(); __key('Space', true); __step(); __key('Space', false);
		const s = playerShot;
		if (!s) return 'pressing space did not fire a shot';
		const pc = player.x + player.w / 2, sc = s.x + s.w / 2;
		if (Math.abs(pc - sc) > 3) return 'the shot left from x ' + sc + ', the middle of the cannon is ' + pc;
		if (s.y + s.h > player.y + 1) return 'the shot starts below the top of the cannon';
		const y1 = s.y; __step(5);
		if (playerShot !== s) return 'the shot vanished in flight';
		const dy = y1 - s.y;
		if (!(dy >= 38 && dy <= 42)) return 'the shot rose ' + dy + 'px in 5 frames, expected 40 (8 a frame)';
		player.x = 10; playerShot = null; player.cooldown = 0; __key('Space', true); __step(); __key('Space', false);
		const t = playerShot; let n = 0; while (playerShot && n < 120) { __step(); n++; }
		if (!t) return 'the cannon could not fire from the left side';
		if (playerShot) return 'a shot that flew off the top of the screen is still in play (y ' + t.y + ')';
		if (player.score !== 0 || __alive().length !== 55) return 'a shot that hit nothing changed the score or killed an alien';
		__key('Space', true); __step();
		if (!playerShot) return 'after a shot left the screen the cannon could not fire again';
		return '';`,

	one_shot: `__arena(); __key('Space', true); __step(); const s = playerShot;
		if (!s) return 'pressing space did not fire a shot';
		__step(20);
		if (playerShot !== s) return 'holding space fired another shot while the first was still in flight';
		return '';`,

	cooldown: `__arena(); __key('Space', true); __step();
		if (!playerShot) return 'pressing space did not fire a shot';
		playerShot = null; let n = 0;
		while (playerShot === null && n < 60) { __step(); n++; }
		if (playerShot === null) return 'after the shot was gone, holding space never fired again';
		if (n < 12) return 'the next shot fired ' + n + ' frame(s) after the last, expected a 15-frame cooldown';
		if (n > 18) return 'the next shot took ' + n + ' frames, expected a 15-frame cooldown';
		return '';`,

	march: `__arena(); const m = __moveUntil(120);
		if (!m) return 'the formation did not move in 120 frames';
		if (m.dy.some((v) => v !== 0)) return 'the first move of a new wave was not sideways';
		if (!__same(m.dx)) return 'the aliens did not move together';
		if (!(m.dx[0] > 0)) return 'a new wave marched ' + (m.dx[0] < 0 ? 'left' : 'nowhere') + ' first, expected right';
		if (m.dx[0] > 16) return 'the formation jumped ' + m.dx[0] + 'px in one step';
		const m2 = __moveUntil(120);
		if (!m2 || m2.dx[0] !== m.dx[0] || m2.dy[0] !== 0) return 'the formation did not keep marching right';
		return '';`,

	edge_step_down: `for (const side of ['right', 'left']) {
			__arena();
			if (side === 'right') { const r = Math.max(...aliens.map((a) => a.x + a.w)); for (const a of aliens) a.x += (__W - 12) - r; fleet.dir = 1; }
			else { const l = Math.min(...aliens.map((a) => a.x)); for (const a of aliens) a.x += 12 - l; fleet.dir = -1; }
			let drop = null;
			for (let k = 0; k < 3 && !drop; k++) {
				const m = __moveUntil(120);
				if (!m) return 'the formation stopped moving at the ' + side + ' edge';
				if (Math.max(...aliens.map((a) => a.x + a.w)) > __W || Math.min(...aliens.map((a) => a.x)) < 0) return 'the formation marched off the ' + side + ' edge of the screen';
				if (m.dy[0] !== 0) drop = m;
			}
			if (!drop) return 'at the ' + side + ' edge the formation did not step down';
			if (!__same(drop.dy) || !(drop.dy[0] >= 8 && drop.dy[0] <= 32)) return 'at the ' + side + ' edge the formation moved down ' + drop.dy[0] + 'px, expected 16 for every alien';
			if (drop.dx.some((v) => v !== 0)) return 'the formation moved sideways in the same step as it stepped down';
			const next = __moveUntil(120);
			const want = side === 'right' ? -1 : 1;
			if (!next || next.dy[0] !== 0 || Math.sign(next.dx[0]) !== want) return 'after stepping down at the ' + side + ' edge the formation did not turn around';
		}
		return '';`,

	speed_up: `__arena();
		const gap = () => { if (!__moveUntil(200)) return Infinity; const m = __moveUntil(200); return m ? m.n : Infinity; };
		const full = gap();
		for (const a of __alive().slice(20)) a.alive = false;
		const mid = gap();
		for (const a of __alive().slice(1)) a.alive = false;
		const one = gap();
		const said = 'frames between steps: ' + full + ' with 55 aliens, ' + mid + ' with 20, ' + one + ' with 1';
		if (!(full > mid && mid > one)) return said + '; the formation must speed up as aliens die';
		if (full < 10) return said + '; a full formation should march slowly';
		if (one > 4) return said + '; the last alien should be fast';
		return '';`,

	shot_hits_alien: `__arena(); const rows = __rows();
		const pick = (y) => aliens.filter((a) => a.y === y).sort((p, q) => p.x - q.x)[5];
		const cases = [[pick(rows[4]), 10, 'bottom-row'], [pick(rows[2]), 20, 'middle-row'], [pick(rows[0]), 30, 'top-row']];
		for (const [a, pts, label] of cases) {
			const s0 = player.score, n0 = __alive().length; __shoot(a); __step();
			if (a.alive) return 'a shot inside a ' + label + ' alien did not kill it';
			if (playerShot) return 'the shot that killed an alien is still flying';
			if (__alive().length !== n0 - 1) return 'one shot killed ' + (n0 - __alive().length) + ' aliens';
			if (player.score - s0 !== pts) return 'a ' + label + ' alien scored ' + (player.score - s0) + ', expected ' + pts;
			__step();
			if (player.score - s0 !== pts) return 'a dead alien scored again';
		}
		return '';`,

	shot_misses: `__arena(); const rows = __rows();
		const row = aliens.filter((a) => a.y === rows[4]).sort((p, q) => p.x - q.x); const a = row[4], b = row[5];
		playerShot = { x: (a.x + a.w + b.x) / 2 - 1, y: a.y + 2, w: 2, h: 10, vy: -8 }; __step();
		if (__alive().length !== 55) return 'a shot passing through the gap between two aliens killed one';
		if (!playerShot) return 'a shot passing through the gap between two aliens was stopped';
		playerShot = { x: a.x + a.w / 2 - 1, y: a.y + 120, w: 2, h: 10, vy: -8 }; __step();
		if (__alive().length !== 55) return 'a shot 100px below the formation killed an alien';
		if (player.score !== 0) return 'a shot that hit nothing scored ' + player.score;
		return '';`,

	bomb_hits_player: `__arena(); __bombAt(player.x + player.w / 2 - 1, player.y + 2); __step();
		if (player.lives !== 2) return 'a bomb on the cannon left ' + player.lives + ' lives, expected 2';
		if (bombs.length) return 'the bomb that hit the cannon is still falling';
		if (gameState !== 'PLAYING') return 'losing one of three lives changed the state to ' + gameState;
		__bombAt(player.x + player.w / 2 - 1, player.y + 2); __step(5);
		if (player.lives !== 2) return 'a bomb right after the cannon was hit cost another life (lives ' + player.lives + ')';
		__arena(); __bombAt(player.x + 200, player.y + 2); __step(3);
		if (player.lives !== 3) return 'a bomb 200px from the cannon cost a life';
		return '';`,

	game_over: `__arena(); player.lives = 1; __bombAt(player.x + player.w / 2 - 1, player.y + 2); __step();
		if (gameState !== 'GAME_OVER') return 'losing the last life left the state at ' + gameState + ', expected GAME_OVER';
		if (player.lives > 0) return 'lives read ' + player.lives + ' at game over';
		const f = player.frameCount, x = player.x; __key('ArrowRight', true); __step(60);
		if (player.frameCount !== f || player.x !== x) return 'the game kept playing after game over';
		const shown = document.getElementById('game-over').style.display;
		if (!shown || shown === 'none') return 'game over did not show the #game-over overlay';
		return '';`,

	shield_shot: `__arena(); const x0 = Math.min(...shieldBlocks.map((b) => b.x));
		const column = shieldBlocks.filter((b) => b.x === x0);
		const top = Math.min(...column.map((b) => b.y)), bottom = Math.max(...column.map((b) => b.y + b.h));
		const others = shieldBlocks.filter((b) => b.x !== x0).map((b) => [b, b.hp]);
		let hits = 0, through = false;
		for (let k = 0; k < 20 && !through; k++) {
			const before = __shieldHp(); playerShot = { x: x0 + 2, y: bottom + 4, w: 2, h: 10, vy: -8 };
			for (let n = 0; n < 8 && playerShot; n++) { __step(); if (playerShot && playerShot.y + playerShot.h < top) { through = true; break; } }
			if (through) break;
			if (playerShot) return 'a shot fired up into a shield neither hit it nor got through';
			const lost = before - __shieldHp();
			if (lost !== 1) return 'one shot took ' + lost + ' points off the shield, expected 1';
			hits++;
		}
		if (!through) return 'twenty shots at the same spot never wore a hole through the shield';
		if (hits < 2) return 'the shield gave way after ' + hits + ' shot(s)';
		if (others.some(([b, hp]) => b.hp !== hp)) return 'shots into one column of a shield damaged other blocks';
		return '';`,

	shield_bomb: `__arena(); const tops = Math.min(...shieldBlocks.map((b) => b.y));
		const row = shieldBlocks.filter((b) => b.y === tops).sort((p, q) => p.x - q.x); const b = row[Math.floor(row.length / 2)];
		const hp0 = b.hp, total = __shieldHp(); __bombAt(b.x + b.w / 2 - 1, b.y - 9);
		let n = 0; while (bombs.length && n < 12) { __step(); n++; }
		if (bombs.length) return 'a bomb falling onto a shield went through it';
		if (!(b.hp < hp0)) return 'a bomb that hit a shield did not damage it';
		if (total - __shieldHp() > hp0) return 'one bomb destroyed more than one shield block';
		if (player.lives !== 3) return 'a bomb stopped by a shield cost a life';
		return '';`,

	bombs_drop: `__arena(); fleet.bombTimer = 1; player.x = 0;
		const seen = new Set(); let most = 0, made = 0, moved = 0;
		const ys = new Map();
		for (let n = 0; n < 400 && gameState === 'PLAYING'; n++) {
			__step(); most = Math.max(most, bombs.length);
			for (const b of bombs) {
				if (ys.has(b) && b.y > ys.get(b)) moved++;
				ys.set(b, b.y);
				if (seen.has(b)) continue;
				seen.add(b); made++;
				const cx = b.x + b.w / 2, live = __alive();
				const owner = live.find((a) => cx >= a.x - 9 && cx <= a.x + a.w + 9 && b.y >= a.y && b.y <= a.y + a.h + 12);
				if (!owner) return 'a bomb appeared at (' + b.x + ', ' + b.y + '), not just under any alien';
				if (live.some((a) => a !== owner && cx >= a.x && cx <= a.x + a.w && a.y > owner.y)) return 'a bomb was dropped by an alien with another alien below it';
			}
		}
		if (made < 3) return 'only ' + made + ' bomb(s) fell in 400 frames';
		if (most > 3) return most + ' bombs were falling at once, the limit is 3';
		if (moved === 0) return 'the bombs did not fall';
		// Bombs due every frame: the limit of 3 in the air must still hold.
		__arena(); player.x = 0; most = 0;
		for (let n = 0; n < 150 && gameState === 'PLAYING'; n++) { fleet.bombTimer = 1; __step(); most = Math.max(most, bombs.length); }
		if (most > 3) return most + ' bombs were falling at once, the limit is 3';
		if (most < 2) __warn('with a bomb due every frame at most ' + most + ' was in the air');
		__arena(); __bombAt(4, __H - 4); __step(5);
		if (bombs.length) return 'a bomb that fell off the bottom of the screen is still in play';
		return '';`,

	wave_clear: `__arena(); const top1 = Math.min(...aliens.map((a) => a.y)); player.score = 500; player.lives = 2;
		const live = __alive(); for (const a of live.slice(1)) a.alive = false; const last = live[0];
		__shoot(last); __step();
		if (last.alive) return 'a shot inside the last alien did not kill it';
		const score = player.score;
		if (gameState === 'PLAYING') __step();
		if (gameState === 'PLAYING') return 'killing the last alien left the state at PLAYING';
		let n = 0; while (gameState !== 'PLAYING' && n < 900) { __tick(); n++; }
		if (gameState !== 'PLAYING') return 'after the wave was cleared the game did not resume (state ' + gameState + ' after 15 s)';
		if (level !== 2) return 'the level is ' + level + ' after clearing wave 1, expected 2';
		if (__alive().length !== 55) return 'wave 2 has ' + __alive().length + ' aliens, expected 55';
		const top2 = Math.min(...aliens.map((a) => a.y));
		if (!(top2 > top1)) return 'wave 2 starts at y ' + top2 + ', no lower than wave 1 (' + top1 + ')';
		if (player.score !== score) return 'the score went from ' + score + ' to ' + player.score + ' between waves';
		if (player.lives !== 2) return 'the lives went from 2 to ' + player.lives + ' between waves';
		if (n < 60) __warn('the next wave started ' + n + ' frames after the last alien died, with no pause');
		return '';`,

	invasion: `__arena(); const low = Math.max(...aliens.map((a) => a.y + a.h)); const dy = (player.y - 6) - low;
		for (const a of aliens) a.y += dy;
		const r = Math.max(...aliens.map((a) => a.x + a.w)); for (const a of aliens) a.x += (__W - 12) - r; fleet.dir = 1;
		__step();
		if (gameState !== 'PLAYING') return 'aliens still above the cannon ended the game (state ' + gameState + ')';
		let n = 0; while (gameState === 'PLAYING' && n < 200) { __step(); n++; }
		if (gameState !== 'GAME_OVER') return 'the aliens marched down to the cannon and the game went on (state ' + gameState + ')';
		return '';`,

	hud: `__arena(); player.score = 1230; player.lives = 2; __step();
		if (__text('score') !== '1230') return 'the #score element shows ' + JSON.stringify(__text('score')) + ' for a score of 1230';
		if (__text('lives') !== '2') return 'the #lives element shows ' + JSON.stringify(__text('lives')) + ' for 2 lives';
		return '';`,
};

function ruleContext(blocks) {
	const clock = { now: 0 };
	const page = loadPage(blocks, clock, seeded(0x5ec7e));
	if (typeof page.startGame === "function" && page.__frames.length === 0) page.startGame();
	vm.runInContext(HELPERS, page, { timeout: 2000 });
	return page;
}

function gameLogic(blocks) {
	const failed = [];
	const warnings = [];
	const names = Object.keys(RULES);
	for (const name of names) {
		let detail;
		let notes = [];
		try {
			const page = ruleContext(blocks);
			try {
				detail = vm.runInContext(`(function () { ${RULES[name]} })()`, page, { filename: "rule", timeout: 3000 });
			} finally {
				notes = vm.runInContext("__warnings.slice()", page, { timeout: 1000 });
			}
		} catch (error) {
			detail = describe(error);
		}
		if (detail !== "") failed.push({ rule: name, detail: String(detail) });
		for (const text of notes) warnings.push({ rule: name, detail: String(text) });
	}
	return { ok: failed.length === 0, passed: names.length - failed.length, total: names.length, failed, warnings };
}

try {
	const html = fs.readFileSync(file, "utf8");
	const blocks = scriptBlocks(html);
	const result = run(html, blocks);
	result.game_logic = result.reached_playing ? gameLogic(blocks) : null;
	console.log(JSON.stringify(result));
} catch (error) {
	console.log(JSON.stringify(verdict({ error: `run_game: ${error.message}`, game_logic: null })));
}
