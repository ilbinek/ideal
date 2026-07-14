/** Helpers for reading/writing scalar keys in Arma 3 server.cfg text. */

export type CfgValueKind = "string" | "number" | "bool";

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * Match a scalar assignment line, optionally followed by an inline // comment.
 * Captures: [1]=leading indent, [2]=trailing whitespace + optional // comment.
 */
function assignmentLineRe(key: string): RegExp {
  return new RegExp(
    `^(\\s*)${escapeRegExp(key)}\\s*=\\s*[^;\\n]*;(\\s*(?://[^\\n]*)?)\\s*$`,
    "gim",
  );
}

/** Read a quoted string assignment: key = "value"; */
export function getCfgString(content: string, key: string): string | undefined {
  const re = new RegExp(
    `^\\s*${escapeRegExp(key)}\\s*=\\s*"([^"]*)"\\s*;`,
    "im",
  );
  const m = content.match(re);
  return m ? m[1] : undefined;
}

/** Read a numeric assignment: key = 32; (also accepts unquoted). */
export function getCfgNumber(content: string, key: string): number | undefined {
  const re = new RegExp(
    `^\\s*${escapeRegExp(key)}\\s*=\\s*(-?\\d+(?:\\.\\d+)?)\\s*;`,
    "im",
  );
  const m = content.match(re);
  if (!m) return undefined;
  const n = Number(m[1]);
  return Number.isFinite(n) ? n : undefined;
}

/** Read a 0/1 boolean assignment. */
export function getCfgBool(content: string, key: string): boolean | undefined {
  const n = getCfgNumber(content, key);
  if (n === undefined) return undefined;
  return n !== 0;
}

function formatValue(value: string | number | boolean, kind: CfgValueKind): string {
  switch (kind) {
    case "string":
      return `"${String(value).replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
    case "number":
      return String(Number(value));
    case "bool":
      return value ? "1" : "0";
  }
}

/**
 * Replace an existing `key = ...;` line, or append one if missing.
 * Preserves unrelated lines and inline // comments after the semicolon.
 * If the key appears more than once (e.g. from earlier append bugs), all
 * matching assignment lines are updated to the new value.
 */
export function setCfgValue(
  content: string,
  key: string,
  value: string | number | boolean,
  kind: CfgValueKind,
): string {
  const formatted = formatValue(value, kind);
  const assignment = `${key} = ${formatted};`;
  const re = assignmentLineRe(key);
  // Use match, not next!==content: unchanged values must not append duplicates.
  if (re.test(content)) {
    re.lastIndex = 0;
    return content.replace(re, `$1${assignment}$2`);
  }
  const trimmed = content.replace(/\s*$/, "");
  const sep = trimmed.length === 0 || trimmed.endsWith("\n") ? "" : "\n";
  return `${trimmed}${sep}${assignment}\n`;
}

export type ServerCfgSimpleForm = {
  hostname: string;
  password: string;
  passwordAdmin: string;
  serverCommandPassword: string;
  maxPlayers: number;
  verifySignatures: number;
  allowedFilePatching: number;
  BattlEye: boolean;
  persistent: boolean;
  drawingOnMap: boolean;
};

export const defaultServerCfgForm: ServerCfgSimpleForm = {
  hostname: "Arma 3 Server",
  password: "",
  passwordAdmin: "",
  serverCommandPassword: "",
  maxPlayers: 32,
  verifySignatures: 2,
  allowedFilePatching: 0,
  BattlEye: true,
  persistent: false,
  drawingOnMap: true,
};

/** Parse known simple-form fields from raw server.cfg text. */
export function parseServerCfgForm(content: string): ServerCfgSimpleForm {
  return {
    hostname: getCfgString(content, "hostname") ?? defaultServerCfgForm.hostname,
    password: getCfgString(content, "password") ?? "",
    passwordAdmin: getCfgString(content, "passwordAdmin") ?? "",
    serverCommandPassword: getCfgString(content, "serverCommandPassword") ?? "",
    maxPlayers: getCfgNumber(content, "maxPlayers") ?? defaultServerCfgForm.maxPlayers,
    verifySignatures:
      getCfgNumber(content, "verifySignatures") ?? defaultServerCfgForm.verifySignatures,
    allowedFilePatching:
      getCfgNumber(content, "allowedFilePatching") ??
      defaultServerCfgForm.allowedFilePatching,
    BattlEye: getCfgBool(content, "BattlEye") ?? defaultServerCfgForm.BattlEye,
    persistent: getCfgBool(content, "persistent") ?? defaultServerCfgForm.persistent,
    drawingOnMap: getCfgBool(content, "drawingOnMap") ?? defaultServerCfgForm.drawingOnMap,
  };
}

/** Apply simple-form values into raw server.cfg text (upsert keys). */
export function applyServerCfgForm(
  content: string,
  form: ServerCfgSimpleForm,
): string {
  let out = content;
  out = setCfgValue(out, "hostname", form.hostname, "string");
  out = setCfgValue(out, "password", form.password, "string");
  out = setCfgValue(out, "passwordAdmin", form.passwordAdmin, "string");
  out = setCfgValue(out, "serverCommandPassword", form.serverCommandPassword, "string");
  out = setCfgValue(out, "maxPlayers", form.maxPlayers, "number");
  out = setCfgValue(out, "verifySignatures", form.verifySignatures, "number");
  out = setCfgValue(out, "allowedFilePatching", form.allowedFilePatching, "number");
  out = setCfgValue(out, "BattlEye", form.BattlEye, "bool");
  out = setCfgValue(out, "persistent", form.persistent, "bool");
  out = setCfgValue(out, "drawingOnMap", form.drawingOnMap, "bool");
  return out;
}
