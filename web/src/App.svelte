<script lang="ts">
  import Layout from './components/Layout.svelte'
  import HomeView from './views/HomeView.svelte'
  import OnboardingView from './views/OnboardingView.svelte'
  import NoteListView from './views/NoteListView.svelte'
  import NoteDetailView from './views/NoteDetailView.svelte'
  import NoteEditorView from './views/NoteEditorView.svelte'
  import EncryptView from './views/EncryptView.svelte'
  import ForgotPasswordView from './views/ForgotPasswordView.svelte'
  import NotFoundView from './views/NotFoundView.svelte'
  import { loadSettings, settings } from './lib/settings'
  import { route, type RouteName } from './router/router'

  const titles: Record<RouteName, string> = {
    home: '使用概览',
    list: '全部笔记',
    new: '新建笔记',
    detail: '笔记详情',
    edit: '编辑笔记',
    encrypt: '加密笔记',
    forgot: '忘记密码',
    notFound: '页面不存在',
  }

  $effect(() => {
    loadSettings()
  })

  $effect(() => {
    document.title = $settings.nameSet
      ? `${titles[$route.name]} · {$settings.name}的笔记`
      : '笔记 · 欢迎'
  })
</script>

{#if $settings.status === 'loading'}
  <div class="boot" role="status">正在打开笔记本…</div>
{:else if $settings.status === 'error'}
  <div class="boot" role="alert">
    <p>{$settings.error}</p>
    <button class="button" type="button" onclick={loadSettings}>重新载入</button>
  </div>
{:else if !$settings.nameSet}
  <OnboardingView />
{:else}
  <Layout>
    {#if $route.name === 'home'}
      <HomeView />
    {:else if $route.name === 'list'}
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
{/if}
