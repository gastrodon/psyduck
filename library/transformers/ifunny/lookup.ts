const { Post } = require("ifunny");

export const lookup_content = async (ref: any): Promise<any> => {
  const post = new Post(ref.id);
  await post.fresh.get("");

  return post;
};
