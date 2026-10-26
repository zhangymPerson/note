<script lang="ts">
  let { id }: { id?: string } = $props()
</script>

<section class="page">
  <h1>{id ? '编辑笔记' : '新建笔记'}</h1>
  <p class="muted">{id ? `笔记 ID：${id}` : '创建表单将在 M1 阶段接入。'}</p>
  <div class="card">
    <p>标题与正文编辑表单尚未实现。</p>
  </div>
</section>
