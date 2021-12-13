const { Client } = require("ifunny");

import job from "./library/job";
import configure from "./library/tools/configure";
import { ConfigKind } from "./library/types/config-kind";

const config = configure();
let client = new Client();

client.login(config.get(ConfigKind.Email), config.get(ConfigKind.Password))
  .then(async () => {
    console.log(config);
    // job(config.get(ConfigKind.job))(config);
  });
