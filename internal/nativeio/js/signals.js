((api) => {
  const done = () => ({value: undefined, done: true});

  function subscribe(name) {
    const native = api.subscribe(name);
    return {
      next() {
        return native.next();
      },
      close() {
        return native.close();
      },
      return() {
        return native.close().then(done);
      },
      [Symbol.asyncIterator]() {
        return this;
      },
    };
  }

  return {subscribe};
})
