import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { tags as t } from "@lezer/highlight";
import type { Extension } from "@codemirror/state";

// GitHub Dimmed palette (matches fe-theme-dimmed in theme.css).
// Source: https://primer.style/primitives/colors (dark dimmed).
const githubDimmedStyle = HighlightStyle.define([
  { tag: t.keyword, color: "#f47067" },
  { tag: t.controlKeyword, color: "#f47067" },
  { tag: t.operatorKeyword, color: "#f47067" },
  { tag: t.definitionKeyword, color: "#f47067" },
  { tag: t.modifier, color: "#f47067" },

  { tag: t.string, color: "#96d0ff" },
  { tag: t.special(t.string), color: "#96d0ff" },
  { tag: t.regexp, color: "#96d0ff" },
  { tag: t.escape, color: "#96d0ff" },

  { tag: [t.comment, t.lineComment, t.blockComment, t.docComment], color: "#768390", fontStyle: "italic" },

  { tag: t.number, color: "#6cb6ff" },
  { tag: t.bool, color: "#6cb6ff" },
  { tag: t.atom, color: "#6cb6ff" },

  { tag: t.function(t.variableName), color: "#dcbdfb" },
  { tag: t.function(t.propertyName), color: "#dcbdfb" },
  { tag: t.macroName, color: "#dcbdfb" },

  { tag: t.variableName, color: "#adbac7" },
  { tag: t.propertyName, color: "#adbac7" },

  { tag: t.typeName, color: "#ffaf6f" },
  { tag: t.className, color: "#ffaf6f" },
  { tag: t.namespace, color: "#ffaf6f" },

  { tag: t.operator, color: "#f47067" },
  { tag: t.punctuation, color: "#adbac7" },
  { tag: t.bracket, color: "#adbac7" },
  { tag: t.derefOperator, color: "#adbac7" },

  // Markup (markdown / HTML)
  { tag: t.tagName, color: "#8ddb8c" },
  { tag: t.attributeName, color: "#dcbdfb" },
  { tag: t.attributeValue, color: "#96d0ff" },

  { tag: t.heading, color: "#6cb6ff", fontWeight: "bold" },
  { tag: t.strong, fontWeight: "bold" },
  { tag: t.emphasis, fontStyle: "italic" },
  { tag: t.link, color: "#6cb6ff", textDecoration: "underline" },
  { tag: t.url, color: "#6cb6ff" },
  { tag: t.quote, color: "#768390" },
  { tag: t.monospace, color: "#96d0ff" },

  { tag: t.invalid, color: "#ff938a" },
  { tag: t.deleted, color: "#ff938a", backgroundColor: "rgba(244,112,103,0.15)" },
  { tag: t.inserted, color: "#8ddb8c", backgroundColor: "rgba(141,219,140,0.15)" },
]);

// GitHub Light palette (matches fe-theme-light).
const githubLightStyle = HighlightStyle.define([
  { tag: t.keyword, color: "#cf222e" },
  { tag: t.controlKeyword, color: "#cf222e" },
  { tag: t.operatorKeyword, color: "#cf222e" },
  { tag: t.definitionKeyword, color: "#cf222e" },
  { tag: t.modifier, color: "#cf222e" },

  { tag: t.string, color: "#0a3069" },
  { tag: t.special(t.string), color: "#0a3069" },
  { tag: t.regexp, color: "#0a3069" },
  { tag: t.escape, color: "#0a3069" },

  { tag: [t.comment, t.lineComment, t.blockComment, t.docComment], color: "#6e7781", fontStyle: "italic" },

  { tag: t.number, color: "#0550ae" },
  { tag: t.bool, color: "#0550ae" },
  { tag: t.atom, color: "#0550ae" },

  { tag: t.function(t.variableName), color: "#8250df" },
  { tag: t.function(t.propertyName), color: "#8250df" },
  { tag: t.macroName, color: "#8250df" },

  { tag: t.variableName, color: "#24292f" },
  { tag: t.propertyName, color: "#24292f" },

  { tag: t.typeName, color: "#953800" },
  { tag: t.className, color: "#953800" },
  { tag: t.namespace, color: "#953800" },

  { tag: t.operator, color: "#cf222e" },
  { tag: t.punctuation, color: "#24292f" },
  { tag: t.bracket, color: "#24292f" },
  { tag: t.derefOperator, color: "#24292f" },

  { tag: t.tagName, color: "#116329" },
  { tag: t.attributeName, color: "#8250df" },
  { tag: t.attributeValue, color: "#0a3069" },

  { tag: t.heading, color: "#0550ae", fontWeight: "bold" },
  { tag: t.strong, fontWeight: "bold" },
  { tag: t.emphasis, fontStyle: "italic" },
  { tag: t.link, color: "#0969da", textDecoration: "underline" },
  { tag: t.url, color: "#0969da" },
  { tag: t.quote, color: "#6e7781" },
  { tag: t.monospace, color: "#0a3069" },

  { tag: t.invalid, color: "#82071e" },
  { tag: t.deleted, color: "#82071e", backgroundColor: "rgba(255,129,130,0.2)" },
  { tag: t.inserted, color: "#116329", backgroundColor: "rgba(74,194,107,0.2)" },
]);

export function highlightExtensionFor(theme: "dimmed" | "light"): Extension {
  return syntaxHighlighting(theme === "light" ? githubLightStyle : githubDimmedStyle);
}
