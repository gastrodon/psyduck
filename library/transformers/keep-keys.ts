export default (keys: Array<string>): (it: any) => any =>
  (it: any) =>
    Object.fromEntries(keys.map(
      (name: string) => [name, it._object_payload[name]],
    ));
