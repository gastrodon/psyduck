export default (it: any) =>
  new Map(
    Object.entries(it)
      .map(([key, value]) => [key, value ?? null]),
  );
