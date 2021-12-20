export const sync = <T>(iterable: Iterable<T>, count: number = 0): Array<T> => {
  let buffer = new Array<T>();
  let pushed = 0;

  for (let it of iterable) {
    buffer.push(it);

    if (count !== 0 && count <= pushed) {
      break;
    }
  }

  return buffer;
};

export const async = async <T>(
  iterable: Iterable<T>,
  count: number = 0,
): Promise<Array<T>> => {
  let buffer = new Array<T>();
  let pushed = 0;

  for await (let it of iterable) {
    buffer.push(it);
    pushed++;

    if (count !== 0 && count <= pushed) {
      break;
    }
  }

  return buffer;
};
