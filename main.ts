import do_job from "./library/jobs";
import configure from "./library/tools/configure";
import { ConfigKind } from "./library/types/config-kind";

do_job(config.get(ConfigKind.Job)!)(configure());
