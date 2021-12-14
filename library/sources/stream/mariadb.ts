import { createPool } from "mariadb";

import { sync as iterate } from "../../tools/iterate";
import Config from "../../types/config";
import AsyncStream from "../../types/async-stream";
import AsyncPool from "../../types/async-pool";
import { StreamConfig } from "../../types/stream-kind";
import { ConfigKind } from "../../types/config-kind";

type DatabaseInsertable = Map<string, string | number | null>;

const ensure_table = async (
  pool: any,
  config: Config,
  stream: StreamConfig,
) => {
  const schema = config.get(ConfigKind.MariadbTableSchema)!;
  const schema_flat = iterate(schema.entries()) as Array<Array<string>>;
  const statement = [
    "CREATE TABLE IF NOT EXISTS",
    stream.name.split("/")[1],
    "(",
    schema_flat.map((it: Array<string>) => it.join(" ")).join(","),
    ")",
  ].join(" ");

  await pool.query(statement);
};

const push_database = async (
  config: Config,
  stream: StreamConfig,
): Promise<(data: DatabaseInsertable) => void> => {
  const pool = await createPool({
    host: config.get(ConfigKind.MariadbHost),
    user: config.get(ConfigKind.MariadbUsername),
    password: config.get(ConfigKind.MariadbPassword),
    database: config.get(ConfigKind.MariadbDatabase),
  });

  await ensure_table(pool, config, stream);
  const table = stream.name.split("/")[1];

  return async (data: DatabaseInsertable) => {
    const keys = iterate(data.keys()).join(", ");
    const marks = iterate(data.keys()).map((_) => "?").join(", ");
    const statement = `INSERT INTO ${table} (${keys}) VALUES (${marks})`;

    pool.query(statement, iterate(data.values()));
  };
};

async function* iterate_database(
  config: Config,
  stream: StreamConfig,
): { [key: string]: any } {
  const pool = await createPool({
    host: config.get(ConfigKind.MariadbHost),
    user: config.get(ConfigKind.MariadbUsername),
    password: config.get(ConfigKind.MariadbPassword),
    database: config.get(ConfigKind.MariadbDatabase),
  });

  await ensure_table(pool, config, stream);
  const table = stream.name.split("/")[1];

  // TODO need some way to not re-visit items
  // and to configure chunked selecting
  for (const row of await pool.query(`SELECT * FROM ${table}`)) {
    yield row;
  }
}

export const read = async (
  config: Config,
  stream: StreamConfig,
): Promise<AsyncStream<any>> => ({
  iterator: await iterate_database(config, stream),
});

export const write = async (
  config: Config,
  stream: StreamConfig,
): Promise<AsyncPool<DatabaseInsertable>> => ({
  push: await push_database(config, stream),
});
