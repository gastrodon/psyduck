import Config from "../types/config";
import { JobKind } from "../types/job-kind";
import { ConfigKind } from "../types/config-kind";

const jobs = new Map([
  [JobKind.QueueLoadStream, require("./queue/load_stream").default],
]);

export default (job: JobKind) => (config: Config) => jobs.get(job)(config);
