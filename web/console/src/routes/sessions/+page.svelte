<script lang="ts">
  import { EmptyState, PageHeader } from '$lib/components';
  import { t } from '$lib/i18n';
</script>

<PageHeader title={$t('sessions.title')} description={$t('sessions.description')} />

<EmptyState title={$t('sessions.empty.title')} description={$t('sessions.empty.description')} />
