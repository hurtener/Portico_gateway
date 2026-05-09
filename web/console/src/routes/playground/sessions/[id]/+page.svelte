<script lang="ts">
  import { page } from '$app/stores';
  import { EmptyState, PageHeader } from '$lib/components';
  import { t } from '$lib/i18n';

  $: sessionId = $page.params.id;
</script>

<PageHeader title={$t('playground.title')} />

<EmptyState
  title={$t('playground.session.id')}
  description={`Session ${sessionId} — replay-as-playground hands off to /playground.`}
/>
