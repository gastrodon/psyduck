const { Comment, Post, User } = require("ifunny");

export const lookup_content = async (ref: any): Promise<any> => {
  const post = new Post(ref.id);
  await post.fresh.get("");

  return post;
};

export const lookup_comment = async (ref: any): Promise<any> => {
  const comment = new Comment(ref.id, ref.cid);
  await comment.fresh.get("");

  return comment;
};

export const lookup_user = async (ref: any): Promise<any> => {
  const user = new User(ref.id);
  await user.fresh.get("");

  return user;
};
