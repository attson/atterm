import type { Extension } from "@codemirror/state";

// Each entry is a dynamic import so the language pack joins the file-explorer
// chunk only when actually needed. Vite splits each `await import(...)` into
// its own chunk root.

function basenameOf(path: string): string {
  const i = path.lastIndexOf("/");
  return i >= 0 ? path.slice(i + 1) : path;
}

async function streamFrom(modeImport: Promise<unknown>, modeKey: string): Promise<Extension> {
  const [{ StreamLanguage }, mod] = await Promise.all([
    import("@codemirror/language"),
    modeImport,
  ]);
  const mode = (mod as Record<string, unknown>)[modeKey];
  return StreamLanguage.define(mode as Parameters<typeof StreamLanguage.define>[0]);
}

export async function languageForPath(path: string): Promise<Extension | null> {
  const base = basenameOf(path);
  // Basename matches (no extension or special).
  switch (base) {
    case "Dockerfile":
      return streamFrom(import("@codemirror/legacy-modes/mode/dockerfile"), "dockerFile");
    case "Gemfile":
    case "Rakefile":
      return streamFrom(import("@codemirror/legacy-modes/mode/ruby"), "ruby");
    case "Makefile":
    case "GNUmakefile":
      // CodeMirror 6 has no makefile mode; clike is close enough for tabs +
      // comments + strings + variables.
      return streamFrom(import("@codemirror/legacy-modes/mode/clike"), "c");
  }

  const m = /\.([A-Za-z0-9]+)$/.exec(base);
  const ext = m ? m[1].toLowerCase() : null;
  if (!ext) return null;

  switch (ext) {
    // Existing 6 — preserved verbatim.
    case "js":
    case "jsx":
    case "ts":
    case "tsx": {
      const { javascript } = await import("@codemirror/lang-javascript");
      return javascript({ typescript: ext === "ts" || ext === "tsx", jsx: ext === "jsx" || ext === "tsx" });
    }
    case "json": {
      const { json } = await import("@codemirror/lang-json");
      return json();
    }
    case "md":
    case "markdown": {
      const { markdown } = await import("@codemirror/lang-markdown");
      return markdown();
    }
    case "css":
    case "scss": {
      const { css } = await import("@codemirror/lang-css");
      return css();
    }
    case "html":
    case "htm": {
      const { html } = await import("@codemirror/lang-html");
      return html();
    }
    case "py": {
      const { python } = await import("@codemirror/lang-python");
      return python();
    }

    // New — official lang packs.
    case "go": {
      const { go } = await import("@codemirror/lang-go");
      return go();
    }
    case "rs": {
      const { rust } = await import("@codemirror/lang-rust");
      return rust();
    }
    case "c":
    case "cc":
    case "cpp":
    case "cxx":
    case "h":
    case "hpp":
    case "hh":
    case "m":
    case "mm": {
      const { cpp } = await import("@codemirror/lang-cpp");
      return cpp();
    }
    case "java": {
      const { java } = await import("@codemirror/lang-java");
      return java();
    }
    case "php": {
      const { php } = await import("@codemirror/lang-php");
      return php();
    }
    case "sql": {
      const { sql } = await import("@codemirror/lang-sql");
      return sql();
    }
    case "xml":
    case "xsd":
    case "xsl":
    case "plist":
    case "svg": {
      const { xml } = await import("@codemirror/lang-xml");
      return xml();
    }
    case "yml":
    case "yaml": {
      const { yaml } = await import("@codemirror/lang-yaml");
      return yaml();
    }
    case "vue": {
      const { vue } = await import("@codemirror/lang-vue");
      return vue();
    }
    case "sass": {
      const { sass } = await import("@codemirror/lang-sass");
      return sass();
    }

    // New — legacy modes (StreamLanguage).
    case "sh":
    case "bash":
    case "zsh":
    case "fish":
    case "ksh":
      return streamFrom(import("@codemirror/legacy-modes/mode/shell"), "shell");
    case "toml":
      return streamFrom(import("@codemirror/legacy-modes/mode/toml"), "toml");
    case "rb":
      return streamFrom(import("@codemirror/legacy-modes/mode/ruby"), "ruby");
    case "lua":
      return streamFrom(import("@codemirror/legacy-modes/mode/lua"), "lua");
    case "ini":
    case "properties":
    case "conf":
      return streamFrom(import("@codemirror/legacy-modes/mode/properties"), "properties");
    case "diff":
    case "patch":
      return streamFrom(import("@codemirror/legacy-modes/mode/diff"), "diff");
    case "swift":
      return streamFrom(import("@codemirror/legacy-modes/mode/swift"), "swift");
    case "kt":
    case "kts":
    case "scala":
      return streamFrom(import("@codemirror/legacy-modes/mode/clike"), "kotlin");

    default:
      return null;
  }
}
