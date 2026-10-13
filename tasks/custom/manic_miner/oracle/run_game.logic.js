// Does the page actually run?
//
// `node --check` only says the file parses, and the test source parses fine --
// it is missing two dozen functions, not a bracket. So the question "is it
// fixed" has to be answered by running it: build a stub DOM and canvas, execute
// every <script> block, start the game and pump a few animation frames. A
// ReferenceError for a function the model never wrote surfaces here and nowhere
// else.
//
//   node smoke.js <file.html> [frames]
//
// Prints one JSON object:
//   { ok, error, frames_run, started, state, player_frames, reached_playing }
//
// FRAMES DEFAULT IS 400, AND THAT IS THE WHOLE POINT.
//
// The game opens in a COUNTDOWN state and `update()` returns early for every
// frame of it. performance.now() here is framesRun * 16.7, the countdown steps
// once per 1000ms, and it starts at 3 -- so gameState flips to 'PLAYING' at
// frame 180 and the physics path first executes at frame 181. The old default
// of 30 frames never left the countdown. Nothing downstream of it ever ran:
// not collide(), not the enemy or item sweeps, not the camera.
//
// That made the oracle blind to the campaign's own injected fault. On
// 2026-09-11 run 0208 restored two of the three deleted functions, left
// `collide` called-but-undefined, and scored {"ok":true} -- a file that throws
// ReferenceError the instant the game actually starts. Its verdict was FIXED.
//
// So: pump past the countdown, and require that the physics actually ran.
// `player.frameCount` increments only on the physics path, which makes it the
// one unforgeable witness that the game is playing rather than merely loading.

const fs = require("node:fs");
const vm = require("node:vm");

const file = process.argv[2];
const FRAMES = Number(process.argv[3] || 400);

/** Everything a canvas context is asked to do, swallowed. */
function canvasContext() {
	return new Proxy(
		{},
		{
			get(target, prop) {
				if (prop in target) {
					return target[prop];
				}
				if (prop === "canvas") {
					return { width: 640, height: 480 };
				}
				// Property reads that game code does arithmetic on.
				if (
					prop === "globalAlpha" ||
					prop === "lineWidth" ||
					prop === "shadowBlur"
				) {
					return 1;
				}
				return (...args) => {
					// measureText is the one whose return value gets used.
					if (prop === "measureText") {
						return { width: (String(args[0] ?? "").length || 1) * 6 };
					}
					if (prop === "createLinearGradient" || prop === "createRadialGradient") {
						return { addColorStop() {} };
					}
					return undefined;
				};
			},
			set(target, prop, value) {
				target[prop] = value;
				return true;
			},
		},
	);
}

function element(id) {
	const el = {
		id,
		width: 640,
		height: 480,
		style: {},
		dataset: {},
		children: [],
		textContent: "",
		innerHTML: "",
		className: "",
		classList: { add() {}, remove() {}, toggle() {}, contains: () => false },
		getContext: () => canvasContext(),
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
		getBoundingClientRect: () => ({
			left: 0,
			top: 0,
			width: 640,
			height: 480,
			right: 640,
			bottom: 480,
		}),
	};
	return el;
}

// A fresh page context. `clock.now` is what performance.now() returns and
// `frames` is where scheduled callbacks queue; run() pumps them, the logic
// traps below never do -- they call update() themselves.
function makeSandbox(clock, frames, random) {
	const elements = new Map();
	const listeners = [];
	const getElementById = (id) => {
		if (!elements.has(id)) {
			elements.set(id, element(id));
		}
		return elements.get(id);
	};
	const requestAnimationFrame = (cb) => {
		frames.pending.push(cb);
		return frames.pending.length;
	};

	const audioNode = () =>
		new Proxy(
			{},
			{
				get(target, prop) {
					if (prop === "value") return 0;
					if (prop === "frequency" || prop === "gain" || prop === "detune") {
						return {
							value: 0,
							setValueAtTime() {},
							linearRampToValueAtTime() {},
							exponentialRampToValueAtTime() {},
							setTargetAtTime() {},
						};
					}
					if (prop in target) return target[prop];
					return () => audioNode();
				},
				set(target, prop, value) {
					target[prop] = value;
					return true;
				},
			},
		);

	const AudioContextStub = function () {
		return {
			currentTime: 0,
			destination: audioNode(),
			state: "running",
			createOscillator: () => audioNode(),
			createGain: () => audioNode(),
			createBiquadFilter: () => audioNode(),
			createBufferSource: () => audioNode(),
			createBuffer: () => ({ getChannelData: () => new Float32Array(128) }),
			resume: () => Promise.resolve(),
			close: () => Promise.resolve(),
		};
	};

	const document = {
		getElementById,
		querySelector: (sel) => getElementById(String(sel).replace(/^[#.]/, "")),
		querySelectorAll: () => [],
		createElement: (tag) => element(tag),
		addEventListener(type, fn) {
			listeners.push([type, fn]);
		},
		removeEventListener() {},
		body: element("body"),
		documentElement: element("html"),
	};

	// The traps seed Math.random so a level is the same on every run; the
	// load check keeps the host's, as it always had.
	let math = Math;
	if (random) {
		math = Object.create(Math);
		math.random = random;
	}

	const sandbox = {
		document,
		console: { log() {}, warn() {}, error() {}, info() {} },
		requestAnimationFrame,
		cancelAnimationFrame() {},
		setTimeout: (cb) => {
			if (typeof cb === "function") frames.pending.push(cb);
			return 0;
		},
		clearTimeout() {},
		setInterval: () => 0,
		clearInterval() {},
		performance: { now: () => clock.now },
		Date,
		Math: math,
		JSON,
		AudioContext: AudioContextStub,
		webkitAudioContext: AudioContextStub,
		Image: function () {
			return element("img");
		},
		alert() {},
		localStorage: {
			getItem: () => null,
			setItem() {},
			removeItem() {},
		},
	};
	sandbox.window = sandbox;
	sandbox.globalThis = sandbox;
	// The traps press keys through the page's own listeners, as a browser
	// would, so they do not depend on what the page calls its input state.
	sandbox.__listeners = listeners;
	vm.createContext(sandbox);
	return sandbox;
}

function scriptBlocks(html) {
	return [...html.matchAll(/<script\b[^>]*>([\s\S]*?)<\/script>/gi)]
		.map((m) => m[1])
		.filter((b) => b.trim());
}

function run(html, blocks) {
	if (blocks.length === 0) {
		// Two very different failures used to share one message, and both have
		// happened: a file truncated above the script (14,127 bytes down to 572,
		// nothing left but head and style) and a file whose script is present but
		// never closed, so the pair this regex needs does not exist. The first is
		// the file being destroyed; the second is an edit that dropped the
		// trailing tags. Reading "no script blocks" as the first cost two
		// investigations, so the verdict now says which one it is.
		const opened = (html.match(/<script\b[^>]*>/gi) || []).length;
		const closed = (html.match(/<\/script>/gi) || []).length;
		const error =
			opened > closed
				? `unclosed <script> (${opened} opened, ${closed} closed)`
				: "no script blocks";
		return {
			ok: false,
			error,
			frames_run: 0,
			started: false,
			state: null,
			player_frames: 0,
			reached_playing: false,
		};
	}

	// Frames are pumped by hand rather than by a timer so the run is bounded and
	// deterministic: whatever the game schedules, we call it FRAMES times.
	const frames = { pending: [] };
	let framesRun = 0;
	const clock = {
		get now() {
			return framesRun * 16.7;
		},
	};
	const sandbox = makeSandbox(clock, frames, null);

	try {
		for (const block of blocks) {
			vm.runInContext(block, sandbox, { timeout: 10000 });
		}
	} catch (error) {
		return {
			ok: false,
			error: `${error.name}: ${error.message}`,
			frames_run: 0,
			started: false,
			state: null,
			player_frames: 0,
			reached_playing: false,
		};
	}

	// Some pages start themselves; if this one waits to be told, tell it.
	let started = false;
	try {
		if (typeof sandbox.startGame === "function" && frames.pending.length === 0) {
			sandbox.startGame();
			started = true;
		}
	} catch (error) {
		return {
			ok: false,
			error: `startGame: ${error.name}: ${error.message}`,
			frames_run: 0,
			started: false,
			state: null,
			player_frames: 0,
			reached_playing: false,
		};
	}

	try {
		while (framesRun < FRAMES) {
			const due = frames.pending;
			frames.pending = [];
			if (due.length === 0) {
				break;
			}
			for (const cb of due) {
				cb(framesRun * 16.7);
			}
			framesRun += 1;
		}
	} catch (error) {
		return {
			ok: false,
			error: `frame ${framesRun}: ${error.name}: ${error.message}`,
			frames_run: framesRun,
			started,
			state: null,
			player_frames: null,
			reached_playing: false,
		};
	}

	// A game that never schedules a frame is not running, however cleanly it
	// loaded -- that is the whole complaint in "it's not working".
	if (framesRun === 0) {
		return {
			ok: false,
			error: "no animation frame was ever scheduled",
			frames_run: 0,
			started,
			state: null,
			player_frames: 0,
			reached_playing: false,
		};
	}

	// The game's top-level `let`/`const` bindings are not properties of the
	// sandbox object, but they ARE in the context's global lexical scope, so a
	// second script in the same context can read them.
	const probe = (expr) => {
		try {
			return vm.runInContext(expr, sandbox, { timeout: 2000 });
		} catch {
			return null;
		}
	};
	const state = probe("typeof gameState === 'string' ? gameState : null");
	const playerFrames = probe(
		"(typeof player === 'object' && player && typeof player.frameCount === 'number') ? player.frameCount : null",
	);
	const reachedPlaying = playerFrames !== null && playerFrames > 0;

	// Loading without throwing is not the bar. The complaint was "it's not
	// working", and a game stuck in its countdown forever is not working.
	if (!reachedPlaying) {
		return {
			ok: false,
			error:
				state === null
					? `physics never ran: player.frameCount unreadable after ${framesRun} frames`
					: `physics never ran: still in ${state} after ${framesRun} frames (countdown ends at frame 180)`,
			frames_run: framesRun,
			started,
			state,
			player_frames: playerFrames,
			reached_playing: false,
		};
	}

	return {
		ok: true,
		error: null,
		frames_run: framesRun,
		started,
		state,
		player_frames: playerFrames,
		reached_playing: true,
	};
}

// ---------------------------------------------------------------------------
// game_logic: does the game still play by its own rules?
//
// Reaching PLAYING proves the physics path ran, not that it is the game. A
// rewrite can get there with `function collide() { return false; }`, a
// `setupLevel` that never calls `pop()`, or an `update()` cut down to
// `player.frameCount++` -- and every one of those has shipped as FIXED. So
// each trap below loads the page into a fresh context, builds a test cavern
// of its own (a flat floor, or a floor and a wall), puts the player in one
// situation, calls `update()` and compares the result with the rules the
// source itself states in `update()`, `Level.gen()` and `Level.pop()`:
// gravity 0.45 capped at 8, friction x0.92, a -9 jump from the ground only,
// 0.6 acceleration capped at 5, tile collision, gems for their points, O2
// canisters capped at 100, a 300-frame power-up, 15 O2 per enemy hit or 25
// points with the power-up, the exit bonus of 50 per cavern and the win at
// cavern 20.
//
// The traps drive behaviour, never text: a rewrite in any style passes if
// the game behaves as the source says it does. The source's own collide()
// test is `x + w - 1`, so a body may sit up to 1px into a tile; the traps
// allow that and nothing more.
//
// Math.random is seeded, so every run builds the same caverns.

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

const HELPERS = `
const __warnings = [];
function __warn(text) { __warnings.push(text); }
function __near(a, b, eps) { return typeof a === 'number' && Math.abs(a - b) <= (eps || 0.001); }
const __codes = { ArrowLeft: 37, ArrowUp: 38, ArrowRight: 39 };
function __key(k, down) {
	const ev = { type: down ? 'keydown' : 'keyup', key: k, code: k, keyCode: __codes[k], which: __codes[k], repeat: false, preventDefault() {}, stopPropagation() {} };
	for (const [type, fn] of __listeners) if (type === ev.type) fn(ev);
}
function __step(n) { for (let i = 0; i < (n || 1); i++) update(); }
// Real time: one frame, and the clock 16.7ms on, as run_game's load check pumps them.
function __tick(n) { for (let i = 0; i < (n || 1); i++) { __clock.now += 16.7; update(); } }
function __flat() {
	const L = currentLevelObj;
	const g = [];
	for (let y = 0; y < L.height; y++) g.push(Array(L.width).fill(y === L.height - 1 ? 1 : 0));
	L.data = g; L.en = []; L.it = []; L.o2c = []; L.pw = []; L.dc = []; L.bs = false; L.boss = null;
	gameState = 'PLAYING'; gameOverFlag = false; transitionTimer = 0; currentLevel = 1;
	player.x = 240; player.y = (L.height - 1) * TILE_SIZE - player.height;
	player.vx = 0; player.vy = 0; player.grounded = true;
	player.score = 0; player.o2 = 100; player.isSuper = false; player.powerupTimer = 0;
	__key('ArrowLeft', false); __key('ArrowRight', false); __key('ArrowUp', false);
}
// On the floor, at rest, where the page's own physics puts the player.
function __settle() { __flat(); __step(8); player.o2 = 100; player.score = 0; }
function __center() { return { x: player.x + player.width / 2, y: player.y + player.height / 2 }; }
// What one frame of doing nothing costs in O2: 0 in the source, a small
// drain in a page that adds an air supply. Pickups are judged net of it.
function __drain() { const o = player.o2; __step(); const d = player.o2 - o; player.o2 = 100; return d; }
function __text(id) { const e = document.getElementById(id); return String(e.innerText !== undefined && e.innerText !== '' ? e.innerText : e.textContent); }
`;

const TRAPS = {
	collide_tiles: `__flat(); const L = currentLevelObj; L.data[5][10] = 1;
		// Collision is also judged through play (landing, wall, jump); this
		// trap only asks the page's collide() directly, when it has one.
		if (typeof collide !== 'function') { __warn('no collide() to test directly; collision is judged through play only'); return ''; }
		if (!collide(10 * TILE_SIZE + 2, 5 * TILE_SIZE + 2, 20, 20)) return 'a box inside a solid tile does not collide';
		if (!collide(9 * TILE_SIZE + 10, 5 * TILE_SIZE + 2, 20, 20)) return 'a box reaching into a solid tile from the next one does not collide';
		if (collide(13 * TILE_SIZE, 5 * TILE_SIZE, 20, 20)) return 'a box in empty space collides';
		if (collide(10 * TILE_SIZE + 2, 3 * TILE_SIZE, 20, 20)) return 'a box two tiles above a solid tile collides';
		return '';`,
	gravity: `__flat(); player.y = 24; player.vy = 0; player.grounded = false; __step(10);
		if (!__near(player.vy, 4.5, 0.01)) return 'vy after 10 airborne frames is ' + player.vy + ', expected 4.5 (0.45 a frame)';
		if (player.y - 24 < 20 || player.y - 24 > 25.5) return 'the player fell ' + (player.y - 24).toFixed(2) + 'px in 10 frames, expected about 24.75';
		player.y = 0; player.vy = 7.9; __step();
		if (!__near(player.vy, 8)) return 'the fall speed is not capped at 8: ' + player.vy;
		return '';`,
	landing: `__flat(); const top = (currentLevelObj.height - 1) * TILE_SIZE; player.y = 40; player.grounded = false; __step(120);
		const bottom = player.y + player.height;
		if (bottom > top + 1) return 'the player sank into the floor: bottom ' + bottom.toFixed(2) + ', floor ' + top;
		if (bottom < top - 9) return 'the player stopped ' + (top - bottom).toFixed(1) + 'px above the floor';
		if (!player.grounded) return 'the player is not grounded on the floor';
		return '';`,
	friction: `__settle(); const x0 = player.x; player.vx = 4; __step();
		if (!__near(player.vx, 3.68)) return 'vx after one frame of coasting is ' + player.vx + ', expected 3.68 (x0.92)';
		if (player.x - x0 < 3.6 || player.x - x0 > 4.01) return 'the player moved ' + (player.x - x0) + 'px, expected about 3.68';
		return '';`,
	run: `__settle(); const x0 = player.x; __key('ArrowRight', true); __step(60);
		if (!(player.vx > 4.5 && player.vx <= 5.0001)) return 'vx after 60 frames holding right is ' + player.vx + ', expected 5 (the cap)';
		if (player.x - x0 < 150) return 'the player moved only ' + (player.x - x0).toFixed(1) + 'px in 60 frames holding right';
		if (gameState !== 'PLAYING') return 'running along an open floor changed the state to ' + gameState;
		__key('ArrowRight', false); __key('ArrowLeft', true); __step(60);
		if (!(player.vx < -4.5 && player.vx >= -5.0001)) return 'vx after 60 frames holding left is ' + player.vx + ', expected -5 (the cap)';
		return '';`,
	jump: `__settle(); const y0 = player.y; __key('ArrowUp', true); let low = 0;
		for (let i = 0; i < 3; i++) { __step(); low = Math.min(low, player.vy); }
		if (!(low <= -8.4 && low >= -9.01)) return 'holding up on the floor for 3 frames gave vy ' + low + ' at most, expected a -9 jump';
		if (!(player.y < y0 - 10)) return 'the player did not rise after jumping';
		__key('ArrowUp', false); __step(200);
		if (!player.grounded) return 'the player never came back down';
		return '';`,
	no_air_jump: `__flat(); player.y = 40; player.vy = 0; player.grounded = false; __key('ArrowUp', true); __step();
		if (player.vy < 0) return 'the player jumped in mid-air (vy ' + player.vy + ')';
		return '';`,
	wall: `__settle(); const L = currentLevelObj, c = 20; for (let y = 0; y < L.height; y++) L.data[y][c] = 1;
		player.x = c * TILE_SIZE - player.width - 30; __key('ArrowRight', true); __step(60);
		const right = player.x + player.width, wall = c * TILE_SIZE;
		if (right > wall + 1) return 'the player ran into the wall: right edge ' + right.toFixed(1) + ', wall ' + wall;
		if (wall - right > 6) return 'the player stopped ' + (wall - right).toFixed(1) + 'px short of the wall';
		return '';`,
	left_edge: `__settle(); player.x = 3; __key('ArrowLeft', true); __step(10);
		if (!(player.x >= 0 && player.x < 1)) return 'x at the left edge is ' + player.x + ', expected 0';
		return '';`,
	gem: `__settle(); const c = __center();
		const it = { x: c.x, y: c.y, tp: 'sapphire', pt: 30, cl: false, fp: 0 };
		const far = { x: c.x + 400, y: c.y, tp: 'ruby', pt: 10, cl: false, fp: 0 };
		currentLevelObj.it.push(it, far); const s0 = player.score; __step();
		if (!it.cl) return 'a gem at the player was not collected';
		if (far.cl) return 'a gem 400px away was collected';
		if (player.score - s0 !== 30) return 'the score rose by ' + (player.score - s0) + ' for a 30-point gem';
		__step();
		if (player.score - s0 !== 30) return 'a collected gem scored again';
		return '';`,
	oxygen: `__settle(); const d = __drain(); const c = __center(); player.o2 = 50;
		const a = { x: c.x, y: c.y, cl: false, ra: 30 }; currentLevelObj.o2c.push(a); __step();
		if (!a.cl || !__near(player.o2, 80 + d, 0.01)) return 'O2 50 plus a 30 canister gave ' + player.o2 + ', expected ' + (80 + d);
		player.o2 = 90; const b = { x: c.x, y: c.y, cl: false, ra: 30 }; currentLevelObj.o2c.push(b); __step();
		if (!(player.o2 <= 100.0001 && player.o2 >= 100 + d - 0.01)) return 'O2 is not capped at 100: ' + player.o2;
		return '';`,
	powerup: `__settle(); const c = __center(); const p = { x: c.x, y: c.y, cl: false };
		currentLevelObj.pw.push(p); __step();
		if (!p.cl || !player.isSuper) return 'the power-up at the player was not taken';
		__step(250);
		if (!player.isSuper) return 'the power-up ended before 250 frames';
		__step(60);
		if (player.isSuper) return 'the power-up still holds after 310 frames';
		return '';`,
	enemy_hit: `__settle(); const d = __drain(); const c = __center();
		const e = { x: c.x - 10, y: c.y - 6, vx: 0, al: true, tp: 'worm', fc: 0 }; currentLevelObj.en.push(e); __step();
		if (!__near(player.o2, 85 + d, 0.01)) return 'O2 after an enemy hit is ' + player.o2 + ', expected ' + (85 + d);
		if (!(player.vy < 0)) return 'an enemy hit gave no knock-back';
		if (!e.al) return 'the enemy died without the power-up';
		return '';`,
	enemy_super: `__settle(); const d = __drain(); const c = __center(); player.isSuper = true; player.powerupTimer = 100;
		const e = { x: c.x - 10, y: c.y - 6, vx: 0, al: true, tp: 'spike', fc: 0 }; currentLevelObj.en.push(e);
		const s0 = player.score; __step();
		if (e.al) return 'an enemy touched with the power-up survived';
		if (player.score - s0 !== 25) return 'defeating an enemy scored ' + (player.score - s0) + ', expected 25';
		if (!__near(player.o2, 100 + d, 0.01)) return 'O2 fell to ' + player.o2 + ' with the power-up on';
		return '';`,
	exit: `__settle(); const L = currentLevelObj; player.x = (L.width - 8) * TILE_SIZE + 5; const s0 = player.score; __step();
		if (gameState !== 'LEVEL_COMPLETE') {
			player.x = (L.width - 2) * TILE_SIZE; __step();
			if (gameState !== 'LEVEL_COMPLETE') return 'at the far end of the cavern the state is ' + gameState + ', expected LEVEL_COMPLETE';
			__warn('the exit is not where the source puts it (8 tiles from the end)');
		}
		if (player.score - s0 !== 50) return 'the level bonus was ' + (player.score - s0) + ', expected 50 on cavern 1';
		return '';`,
	transition: `__flat(); gameState = 'LEVEL_COMPLETE'; transitionTimer = 1; __step();
		if (gameState !== 'SHOW_STARTING_MSG') return 'after the level-complete pause the state is ' + gameState;
		__step(90);
		if (gameState !== 'COUNTDOWN') return 'after the starting message the state is ' + gameState;
		return '';`,
	win: `__flat(); currentLevel = 20; gameState = 'LEVEL_COMPLETE'; transitionTimer = 1; __step();
		const f = player.frameCount; __step(300);
		if (player.frameCount !== f) return 'the game kept playing after cavern 20 was finished';
		const shown = document.getElementById('game-over-overlay').style.display;
		if (!shown || shown === 'none') return 'finishing cavern 20 did not show the end-of-game overlay';
		if (!/WIN/i.test(__text('go-title'))) return 'the end-of-game title reads ' + JSON.stringify(__text('go-title')) + ' after a win';
		return '';`,
	// Entered the way the game enters it (the end of the starting message), and
	// run as a browser runs it: frames at 60 per second with the clock moving
	// 16.7ms a frame, so a countdown that counts frames and one that reads the
	// clock both see real time. The source's lasts 3 seconds; 2 to 4 passes.
	countdown: `__flat(); gameState = 'SHOW_STARTING_MSG'; transitionTimer = 1; __tick(1);
		if (gameState !== 'COUNTDOWN') return 'the end of the starting message led to ' + gameState + ', expected COUNTDOWN';
		let n = 0; while (gameState === 'COUNTDOWN' && n < 600) { __tick(1); n++; }
		if (gameState !== 'PLAYING') return 'ten seconds of countdown left the state at ' + gameState;
		if (n < 120) return 'the countdown ended after ' + (n / 60).toFixed(1) + ' seconds, expected about 3';
		if (n > 240) return 'the countdown took ' + (n / 60).toFixed(1) + ' seconds, expected about 3';
		const f = player.frameCount; __tick(1);
		if (player.frameCount !== f + 1) return 'the physics did not run after the countdown';
		return '';`,
	camera: `__settle(); player.x = 1000; __step();
		if (!__near(camera.x, player.x - 336, 0.01)) return 'the camera is at ' + camera.x + ' with the player at ' + player.x + ', expected ' + (player.x - 336);
		player.x = 10; __step();
		if (camera.x !== 0) return 'the camera is not held at the left edge: ' + camera.x;
		return '';`,
	hud: `__settle(); player.score = 123; player.o2 = 42.7; __step();
		if (__text('score') !== '123') return 'the #score element shows ' + JSON.stringify(__text('score')) + ' for a score of 123';
		if (!/^4[23](\\.\\d+)?%$/.test(__text('o2-val'))) return 'the #o2-val element shows ' + JSON.stringify(__text('o2-val')) + ' for 42.7 O2';
		return '';`,
	level: `setupLevel(1); const L = currentLevelObj;
		if (!(L instanceof Level)) return 'setupLevel did not make a Level';
		if (!Array.isArray(L.data) || L.data.length !== L.height || L.data[L.height - 1].some((v) => v !== 1)) return 'the bottom row is not solid floor';
		const source = L.width === 220 && L.height === 12;
		if (!source) __warn('cavern 1 is ' + L.width + 'x' + L.height + ', the source makes 220x12');
		// pop() places 5 enemies, 11 gems and 3 O2 canisters in cavern 1, each
		// wherever sX() finds an open column. Averaged over 20 caverns: none of
		// a kind is a failure, fewer than 80% is a warning.
		const want = { en: 5, it: 11, o2c: 3 }, got = { en: 0, it: 0, o2c: 0 };
		for (let i = 0; i < 20; i++) { setupLevel(1); for (const k in want) got[k] += currentLevelObj[k].length / 20; }
		const label = { en: 'enemies', it: 'gems', o2c: 'O2 canisters' };
		for (const k in want) {
			if (got[k] < 0.5) return 'cavern 1 gets ' + got[k].toFixed(1) + ' ' + label[k] + ' on average over 20 caverns, the source places ' + want[k];
			if (got[k] < want[k] * 0.8) __warn('cavern 1 gets ' + got[k].toFixed(1) + ' ' + label[k] + ' on average, the source places ' + want[k]);
		}
		setupLevel(4); const w4 = currentLevelObj.width;
		if (!(w4 > L.width)) return 'cavern 4 is ' + w4 + ' wide, no wider than cavern 1 (' + L.width + '): setupLevel ignores its level';
		if (source && w4 !== 340) __warn('cavern 4 is ' + w4 + ' wide, the source makes 340');
		return '';`,
	spawn_columns: `setupLevel(1); const L = currentLevelObj; const seen = new Set(); let empty = 0;
		for (let i = 0; i < 200; i++) {
			const x = L.sX(); if (x === -1) { empty++; continue; }
			if (!Number.isInteger(x) || x < 0 || x >= L.width) return 'sX returned ' + x + ' for a cavern ' + L.width + ' wide';
			let open = false; for (let y = 1; y < L.height - 1; y++) if (L.data[y][x] === 0) open = true;
			if (!open) return 'sX picked column ' + x + ', which has no empty tile';
			seen.add(x);
		}
		if (seen.size < 10) return 'sX picked only ' + seen.size + ' distinct columns in 200 calls';
		if (empty > 10) __warn('sX found no column in ' + empty + ' of 200 calls; the source retries 50 times');
		return '';`,
	clouds: `initClouds();
		if (!Array.isArray(clouds) || clouds.length < 1) return 'initClouds made no clouds';
		if (!clouds.every((c) => c instanceof Cloud)) return 'the clouds are not Cloud objects';
		return '';`,
};

function trapContext(blocks) {
	const frames = { pending: [] };
	const clock = { now: 0 };
	const sandbox = makeSandbox(clock, frames, seeded(0x6d616e));
	sandbox.__clock = clock;
	for (const block of blocks) {
		vm.runInContext(block, sandbox, { timeout: 10000 });
	}
	if (typeof sandbox.startGame === "function" && frames.pending.length === 0) {
		sandbox.startGame();
	}
	vm.runInContext(HELPERS, sandbox, { timeout: 2000 });
	return sandbox;
}

function gameLogic(blocks) {
	const failed = [];
	const warnings = [];
	const names = Object.keys(TRAPS);
	for (const name of names) {
		let detail;
		let notes = [];
		try {
			const sandbox = trapContext(blocks);
			try {
				detail = vm.runInContext(`(function () { ${TRAPS[name]} })()`, sandbox, { timeout: 2000 });
			} finally {
				notes = vm.runInContext("__warnings.slice()", sandbox, { timeout: 2000 });
			}
		} catch (error) {
			detail = `${error.name}: ${error.message}`;
		}
		if (detail !== "") {
			failed.push({ trap: name, detail: String(detail) });
		}
		for (const text of notes) {
			warnings.push({ trap: name, detail: String(text) });
		}
	}
	return {
		ok: failed.length === 0,
		passed: names.length - failed.length,
		total: names.length,
		failed,
		warnings,
	};
}

// `ok` keeps its old meaning -- the page loads and the physics run -- so
// every verdict recorded before game_logic existed still reads the same.
// game_logic is judged only once that holds; a page that does not run has
// no rules to check.
try {
	const html = fs.readFileSync(file, "utf8");
	const blocks = scriptBlocks(html);
	const verdict = run(html, blocks);
	verdict.game_logic = verdict.reached_playing ? gameLogic(blocks) : null;
	console.log(JSON.stringify(verdict));
} catch (error) {
	console.log(
		JSON.stringify({
			ok: false,
			error: `harness: ${error.message}`,
			frames_run: 0,
			started: false,
			state: null,
			player_frames: 0,
			reached_playing: false,
			game_logic: null,
		}),
	);
}
