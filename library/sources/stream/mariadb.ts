import { createPool } from "mariadb";

import iterate from "../../tools/iterate";
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

// TODO can't set rerturn type of (data: any): void
const push_database = async (
  config: Config,
  stream: StreamConfig,
): Promise<any> => {
  const pool = await createPool({
    host: config.get(ConfigKind.MariadbHost),
    user: config.get(ConfigKind.MariadbUsername),
    password: config.get(ConfigKind.MariadbPassword),
    database: config.get(ConfigKind.MariadbDatabase),
  });

  await ensure_table(pool, config, stream);
  return async (data: DatabaseInsertable) => {
    const keys = new Array(data.keys());
    const values = new Array(data.values());
    const statement = `INSERT INTO (${keys.join(",")}) VALUES (${
      keys.map((_) => "?").join(",")
    })`;

    console.log(statement, values);
    // pool.query(statement, values);
  };
};

async function* iterate_database(
  config: Config,
  stream: StreamConfig,
) {
  yield "TODO";
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
