/** A controllable promise for race tests, including CI's Node 20 runtime. */
export function deferred<T>() {
  // The Promise constructor assigns both callbacks synchronously.
  let resolve: (value: T | PromiseLike<T>) => void = () => {};
  let reject: (reason?: unknown) => void = () => {};
  const promise = new Promise<T>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}
