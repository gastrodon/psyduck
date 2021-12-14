export const sync = <T>(iterable: Iterable<T>): Array<T> => {
  let buffer = new Array<T>();

  for (let it of iterable) {
    buffer.push(it);
  }

  return buffer;
};

export const async = async <T>(iterable: Iterable<T>): Promise<Array<T>> => {
  let buffer = new Array<T>();

  for await (let it of iterable) {
    buffer.push(it);
  }

  return buffer;
};
