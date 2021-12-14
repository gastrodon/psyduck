export default (keys: Array<string>): (it: any) => any => {
  return (it: any) => {
    JSON.stringify(Object.fromEntries(keys.map(
      (name: string) => [name, it._object_payload[name]],
    )));
  };
};
