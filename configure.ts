import parser from "args-parser";

export default () => {
  const args = parser(process.argv);

  console.log(args);
};
