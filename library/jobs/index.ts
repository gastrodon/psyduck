import Config from "../types/config";
import { JobKind } from "../types/job-kind";
import { ConfigKind } from "../types/config-kind";

const jobs = new Map([
  [JobKind.QueueLoadFeed, require("./queue/load-feed").default],
]);

export default (job: JobKind) => (config: Config) => jobs.get(job)(config);
