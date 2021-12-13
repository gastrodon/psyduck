const { Client } = require("ifunny");

import { Config, configure } from "./configure";

const config = configure();
let client = new Client();

client.login(config.get(Config.Email), config.get(Config.Password))
  .then(async () => {
    console.log(config);
    // job(config.get(Config.job))(config);
  });
