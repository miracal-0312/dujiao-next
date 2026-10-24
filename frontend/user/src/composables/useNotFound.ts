import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useAppStore } from '../stores/app'

/**
 * 404 页共享逻辑（classic + vault 双模板共用）。
 * 完整保留原 views/NotFound.vue 的行为，仅抽离为 composable。
 */
export function useNotFound() {
  const { t } = useI18n()
  const router = useRouter()
  const appStore = useAppStore()

  const brandSiteName = computed(() => {
    const siteName = String(appStore.config?.brand?.site_name || '').trim()
    return siteName !== '' ? siteName : t('common.siteName')
  })

  const goBack = () => {
    if (window.history.length > 1) {
      router.back()
      return
    }
    router.push('/')
  }

  return {
    brandSiteName,
    goBack,
  }
}
