#!/usr/bin/env node
// pimark bridge: render markdown/code with pi's OWN renderer over JSONL.
//
//   stdin:  {"id":1,"op":"md","text":"...","width":80,"kind":"assistant"}
//           {"id":2,"op":"hl","code":"...","lang":"go"}
//           {"id":3,"op":"ping"}
//   stdout: {"id":1,"lines":["..."]}
//           {"id":2,"error":"..."}
//
// Started by pitago's src/pimark (persistent node process, one per app run).
// pi root + theme come from argv: --pi-root <pi dist dir> --theme dark|light.
import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";
import path from "node:path";
import readline from "node:readline";

function arg(name, def) {
	const i = process.argv.indexOf(name);
	return i >= 0 && i + 1 < process.argv.length ? process.argv[i + 1] : def;
}

const piRoot = arg("--pi-root", "");
const themeName = arg("--theme", "dark");
if (!piRoot) {
	console.error("pimark: --pi-root required");
	process.exit(2);
}

const themeFile = path.join(piRoot, "modes/interactive/theme/theme.js");
const themeMod = await import(pathToFileURL(themeFile).href);
themeMod.initTheme(themeName);
const mdTheme = themeMod.getMarkdownTheme();

// Resolve pi-tui the same way pi itself does (works hoisted or nested).
const requireFromPi = createRequire(themeFile);
const tuiIndex = requireFromPi.resolve("@earendil-works/pi-tui");
const { Markdown } = await import(pathToFileURL(tuiIndex).href);

function send(obj) {
	process.stdout.write(JSON.stringify(obj) + "\n");
}

// Matches pi's AssistantMessageComponent (paddingX=0 here: pitago draws its
// own gutter) and its thinking block (thinkingText + italic).
function renderMd(text, width, kind) {
	const style =
		kind === "thinking"
			? { color: (t) => themeMod.theme.fg("thinkingText", t), italic: true }
			: undefined;
	const md = new Markdown(text, 0, 0, mdTheme, style, {});
	return md.render(Math.max(1, width || 80));
}

const rl = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
for await (const line of rl) {
	if (!line.trim()) continue;
	let req;
	try {
		req = JSON.parse(line);
	} catch {
		continue;
	}
	try {
		if (req.op === "ping") {
			send({ id: req.id, ok: true });
		} else if (req.op === "md") {
			send({ id: req.id, lines: renderMd(req.text ?? "", req.width, req.kind) });
		} else if (req.op === "hl") {
			send({ id: req.id, lines: themeMod.highlightCode(req.code ?? "", req.lang) });
		} else {
			send({ id: req.id, error: "unknown op" });
		}
	} catch (e) {
		send({ id: req.id, error: String((e && e.message) || e) });
	}
}
