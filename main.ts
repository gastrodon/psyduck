const { Client } = require("ifunny");

import do_job from "./library/jobs";
import configure from "./library/tools/configure";
import { ConfigKind } from "./library/types/config-kind";
import { JobKind, lookup as job_lookup } from "./library/types/job-kind";

const config = configure();
let client = new Client();

do_job(config.get(ConfigKind.Job)!)(config);
