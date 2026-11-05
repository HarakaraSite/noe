(() => {
  "use strict";
  class Dirent {
    #kind;
    constructor(name, kind, parentPath) {
      this.name = name;
      this.parentPath = parentPath;
      this.#kind = kind;
    }
    isFile() { return this.#kind === 0o100000; }
    isDirectory() { return this.#kind === 0o040000; }
    isSymbolicLink() { return this.#kind === 0o120000; }
    isFIFO() { return this.#kind === 0o010000; }
    isSocket() { return this.#kind === 0o140000; }
    isCharacterDevice() { return this.#kind === 0o020000; }
    isBlockDevice() { return this.#kind === 0o060000; }
  }
  return {
    Dirent,
    fromEntries(entries, parentPath) {
      return entries.map(entry => new Dirent(entry.name, entry.kind, parentPath));
    },
  };
})()
