<script lang="ts">
  import Layout from './components/Layout.svelte'
  import NoteListView from './views/NoteListView.svelte'
  import NoteDetailView from './views/NoteDetailView.svelte'
  import NoteEditorView from './views/NoteEditorView.svelte'
  import EncryptView from './views/EncryptView.svelte'
  import ForgotPasswordView from './views/ForgotPasswordView.svelte'
  import NotFoundView from './views/NotFoundView.svelte'
  import { route, type RouteName } from './router/router'

  const titles: Record<RouteName, string> = {
    list: '全部笔记',
    new: '新建笔记',
    detail: '笔记详情',
    edit: '编辑笔记',
    encrypt: '加密笔记',
    forgot: '忘记密码',
    notFound: '页面不存在',
  }

  $effect(() => {
    document.title = `${titles[$route.name]} · 笔记`
  })
</script>

<Layout>
  {#if $route.name === 'list'}
    <NoteListView />
  {:else if $route.name === 'new'}
    <NoteEditorView />
  {:else if $route.name === 'detail'}
    <NoteDetailView id={$route.params.id ?? ''} />
  {:else if $route.name === 'edit'}
    <NoteEditorView id={$route.params.id ?? ''} />
  {:else if $route.name === 'encrypt'}
    <EncryptView id={$route.params.id ?? ''} />
  {:else if $route.name === 'forgot'}
    <ForgotPasswordView id={$route.params.id ?? ''} />
  {:else}
    <NotFoundView />
  {/if}
</Layout>
