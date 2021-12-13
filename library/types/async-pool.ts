interface AsyncPool<T> {
  push(value?: T): void;
}

export default AsyncPool;
