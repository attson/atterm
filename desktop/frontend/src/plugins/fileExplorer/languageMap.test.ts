import { describe, expect, it } from "vitest";
import { languageForPath } from "./languageMap";

describe("languageForPath — existing", () => {
  it("returns javascript for .js", async () => {
    expect(await languageForPath("/x/a.js")).not.toBeNull();
  });
  it("returns null for unknown extension", async () => {
    expect(await languageForPath("/x/a.zzz")).toBeNull();
  });
  it("missing extension is null when not a known basename", async () => {
    expect(await languageForPath("/x/LICENSE")).toBeNull();
  });
});

describe("languageForPath — new extensions", () => {
  const exts = [
    "go", "rs",
    "c", "cc", "cpp", "cxx", "h", "hpp", "hh", "m", "mm",
    "java", "kt", "kts", "scala",
    "php", "sql",
    "xml", "xsd", "xsl", "plist", "svg",
    "yml", "yaml",
    "vue", "sass",
    "sh", "bash", "zsh", "fish", "ksh",
    "toml", "rb", "lua",
    "ini", "properties", "conf",
    "diff", "patch", "swift",
  ];
  for (const ext of exts) {
    it(`returns a language for .${ext}`, async () => {
      expect(await languageForPath(`/x/file.${ext}`)).not.toBeNull();
    });
  }
});

describe("languageForPath — basename matches", () => {
  for (const base of ["Dockerfile", "Gemfile", "Rakefile", "Makefile", "GNUmakefile"]) {
    it(`returns a language for basename ${base}`, async () => {
      expect(await languageForPath(`/x/${base}`)).not.toBeNull();
    });
  }
});

describe("languageForPath — case insensitive", () => {
  it("treats uppercase extension same as lowercase", async () => {
    expect(await languageForPath("/x/main.GO")).not.toBeNull();
    expect(await languageForPath("/x/data.YAML")).not.toBeNull();
  });
});
