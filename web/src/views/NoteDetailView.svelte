<script lang="ts">
  let { id }: { id: string } = $props()
</script>

<section class="page">
  <h1>笔记详情</h1>
  <p class="muted">笔记 ID：<code>{id || '未知'}</code></p>
  <div class="card">
    <p>正文、编辑、加密与删除操作将在后续阶段接入。</p>
  </div>
  <a href="/">← 返回列表</a>
</section>
