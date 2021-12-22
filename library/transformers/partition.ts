export default (items: string) =>
  (data: any): Array<{ [key: string]: any }> =>
    data[items].map((it: any) => ({ ...data, [items]: it }));
