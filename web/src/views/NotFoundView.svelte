<script lang="ts">
  import { navigate } from '../router/router'
</script>

<section class="page notfound">
  <h1>404</h1>
  <p class="muted">这一页不存在，或者已经被移走了。</p>
  <a class="button btn-primary" href="/" onclick={(event) => { event.preventDefault(); navigate('/') }}>
    回到概览
  </a>
</section>
