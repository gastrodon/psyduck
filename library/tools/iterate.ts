export default <T>(iterable: Iterable<T>): Array<T> => {
  let buffer = new Array();

  for (let it of iterable) {
    buffer.push(it);
  }

  return buffer;
};
