import type { Extension } from "@codemirror/state";

// Each entry is a dynamic import so the language pack joins the file-explorer
// chunk only when actually needed. Vite static-imports them all into the file
// chunk regardless, but the lazy form keeps each language file as its own
// import root for future splitting.

export async function languageForPath(path: string): Promise<Extension | null> {
  const m = /\.([A-Za-z0-9]+)$/.exec(path);
  const ext = m ? m[1].toLowerCase() : null;
  if (!ext) return null;
  switch (ext) {
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
    default:
      return null;
  }
}
