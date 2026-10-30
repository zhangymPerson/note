// copyText 把文本写进剪贴板：优先异步剪贴板 API（需要安全上下文，本地 127.0.0.1 满足），
// 拿不到或权限被拒时退回隐藏 textarea + execCommand 的老办法。
// 返回 false 表示两条路都失败，由调用方提示用户手动选中。
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // 落到下面的兜底：某些浏览器在非聚焦状态或权限被拒时抛错
  }

  const area = document.createElement("textarea");
  area.value = text;
  area.setAttribute("readonly", "");
  // 不能让它出现在视口里，也不能让页面因聚焦它而滚动
  area.style.position = "fixed";
  area.style.top = "-1000px";
  area.style.opacity = "0";
  try {
    document.body.append(area);
    area.select();
    return document.execCommand("copy");
  } catch {
    return false;
  } finally {
    // 失败路径也要收干净：临时节点留在 DOM 里会漏文本、抢焦点
    area.remove();
  }
}
