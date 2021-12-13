const { Client } = require("ifunny");

import job from "./library/job";
import { ConfigKind, configure } from "./library/tools/configure";

const config = configure();
let client = new Client();

client.login(config.get(ConfigKind.Email), config.get(ConfigKind.Password))
  .then(async () => {
    console.log(config);
    // job(config.get(ConfigKind.job))(config);
  });
