// 站内回跳路径的守卫：?next= 只接受以 / 开头的绝对路径。
// 协议相对地址（//host）与伪协议（javascript:）会让 history.pushState 抛错或跳走外站，
// 因此两个「交棒」入口（设置主密码、编辑模板）共用这一条规则。
export function safeNextPath(raw: string | null): string {
  if (raw === null || raw === "" || !raw.startsWith("/")) return "";
  if (raw.startsWith("//")) return "";
  return raw;
}
