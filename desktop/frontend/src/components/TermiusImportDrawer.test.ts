import { beforeEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import TermiusImportDrawer from './TermiusImportDrawer.vue';
import { resetI18nForTest } from '../i18n';
import type { TermiusImportPreview } from '../lib/api';

const api = vi.hoisted(() => ({
  discoverTermiusProfiles: vi.fn(), selectTermiusDirectory: vi.fn(),
  previewTermiusImport: vi.fn(), cancelTermiusImport: vi.fn(), importTermius: vi.fn(),
}));
vi.mock('../lib/api', () => api);
const preview: TermiusImportPreview = {
  token: 'snapshot',
  hosts: [
    { id: 'host', name: 'server', host: '192.0.2.1', port: '22', user: 'root', key_id: '', has_password: true, tags: [], warnings: [], blocked: false, existing: false, forward_count: 0 },
    { id: 'proxy', name: 'proxy', host: '192.0.2.2', port: '22', user: 'root', key_id: '', has_password: true, tags: [], warnings: ['unsupported_proxy'], blocked: true, existing: false, forward_count: 0 },
    { id: 'existing', name: 'existing', host: '192.0.2.3', port: '22', user: 'root', key_id: '', has_password: true, tags: [], warnings: [], blocked: false, existing: true, forward_count: 0 },
  ],
  keys: [{ id: 'key', name: 'orphan key', valid: true }],
};
beforeEach(() => {
  resetI18nForTest();
  vi.resetAllMocks();
  api.discoverTermiusProfiles.mockResolvedValue(['/synthetic/Termius']);
  api.previewTermiusImport.mockResolvedValue(preview);
  api.cancelTermiusImport.mockResolvedValue(undefined);
  api.importTermius.mockResolvedValue({ hosts: 1, keys: 0, skipped: 0 });
});

describe('Termius import', () => {
  it('requires selection, submits only source IDs, and clears the localKey', async () => {
    const wrapper = mount(TermiusImportDrawer);
    await flushPromises();
    expect(wrapper.get<HTMLInputElement>('[data-test="termius-path"]').element.value).toBe('/synthetic/Termius');
    await wrapper.get('[data-test="termius-local-key"]').setValue('local-key-canary');
    await wrapper.get('[data-test="termius-preview"]').trigger('click');
    await flushPromises();
    expect(api.previewTermiusImport).toHaveBeenCalledWith('/synthetic/Termius', 'local-key-canary');
    expect(wrapper.html()).not.toContain('local-key-canary');
    expect(wrapper.get<HTMLButtonElement>('[data-test="termius-confirm"]').element.disabled).toBe(true);
    expect(wrapper.get<HTMLInputElement>('[data-test="termius-host-proxy"]').element.disabled).toBe(true);
    expect(wrapper.get<HTMLInputElement>('[data-test="termius-host-existing"]').element.disabled).toBe(true);
    await wrapper.get('[data-test="termius-host-host"]').setValue(true);
    await wrapper.get('[data-test="termius-confirm"]').trigger('click');
    await flushPromises();
    expect(api.importTermius).toHaveBeenCalledWith('snapshot', ['host'], []);
    expect(wrapper.emitted('imported')?.[0]).toEqual([{ hosts: 1, keys: 0, skipped: 0 }]);
    wrapper.unmount();
  });
  it('cancels a scan that finishes after closing', async () => {
    let finish!: (p: TermiusImportPreview) => void;
    api.previewTermiusImport.mockReturnValue(new Promise<TermiusImportPreview>(resolve => { finish = resolve; }));
    const wrapper = mount(TermiusImportDrawer);
    await flushPromises();
    await wrapper.get('[data-test="termius-preview"]').trigger('click');
    wrapper.unmount();
    finish(preview);
    await flushPromises();
    expect(api.cancelTermiusImport).toHaveBeenCalledWith('snapshot');
  });
  it('preserves the preview after an import failure for retry', async () => {
    api.importTermius.mockRejectedValueOnce(new Error('termius_keychain_failed'));
    const wrapper = mount(TermiusImportDrawer);
    await flushPromises();
    await wrapper.get('[data-test="termius-preview"]').trigger('click');
    await flushPromises();
    await wrapper.get('[data-test="termius-keys-tab"]').trigger('click');
    await wrapper.get('[data-test="termius-key-key"]').setValue(true);
    await wrapper.get('[data-test="termius-confirm"]').trigger('click');
    await flushPromises();
    expect(wrapper.get('[data-test="termius-error"]').text()).toContain('keychain');
    expect(wrapper.get<HTMLInputElement>('[data-test="termius-key-key"]').element.checked).toBe(true);
    expect(wrapper.get<HTMLButtonElement>('[data-test="termius-confirm"]').element.disabled).toBe(false);
    await wrapper.get('[data-test="termius-confirm"]').trigger('click');
    await flushPromises();
    expect(wrapper.emitted('imported')).toHaveLength(1);
    wrapper.unmount();
  });
  it('explains how to fully quit Termius and allows rescanning', async () => {
    api.previewTermiusImport.mockRejectedValueOnce(new Error('termius_running'));
    const wrapper = mount(TermiusImportDrawer);
    await flushPromises();
    await wrapper.get('[data-test="termius-preview"]').trigger('click');
    await flushPromises();
    expect(wrapper.get('[data-test="termius-error"]').text()).toContain('system tray');
    expect(wrapper.find('[data-test="termius-confirm"]').exists()).toBe(false);
    await wrapper.get('[data-test="termius-preview"]').trigger('click');
    await flushPromises();
    expect(wrapper.find('[data-test="termius-confirm"]').exists()).toBe(true);
    wrapper.unmount();
  });
  it('explains invalid manual localKey without exposing the submitted value', async () => {
    api.previewTermiusImport.mockRejectedValueOnce(new Error('termius_invalid_local_key'));
    const wrapper = mount(TermiusImportDrawer);
    await flushPromises();
    await wrapper.get('[data-test="termius-local-key"]').setValue('invalid-secret-canary');
    await wrapper.get('[data-test="termius-preview"]').trigger('click');
    await flushPromises();
    expect(wrapper.get('[data-test="termius-error"]').text()).toContain('login password');
    expect(wrapper.html()).not.toContain('invalid-secret-canary');
    wrapper.unmount();
  });
  it('selects only eligible search results and retains selections across filters', async () => {
    api.previewTermiusImport.mockResolvedValue({ ...preview, hosts: [...preview.hosts,
      { ...preview.hosts[0], id: 'second', name: 'other', host: '203.0.113.7', tags: ['production'] },
    ] });
    const wrapper = mount(TermiusImportDrawer);
    await flushPromises();
    await wrapper.get('[data-test="termius-preview"]').trigger('click');
    await flushPromises();
    await wrapper.get('[data-test="termius-search"]').setValue('production root');
    expect(wrapper.find('[data-test="termius-host-host"]').exists()).toBe(false);
    await wrapper.get('[data-test="termius-select-visible"]').setValue(true);
    await wrapper.get('[data-test="termius-search"]').setValue('');
    expect(wrapper.get<HTMLInputElement>('[data-test="termius-select-visible"]').element.indeterminate).toBe(true);
    await wrapper.get('[data-test="termius-host-host"]').setValue(true);
    await wrapper.get('[data-test="termius-search"]').setValue('production');
    await wrapper.get('[data-test="termius-select-visible"]').setValue(false);
    await wrapper.get('[data-test="termius-search"]').setValue('does not exist');
    expect(wrapper.text()).toContain('No matching items');
    await wrapper.get('[data-test="termius-confirm"]').trigger('click');
    await flushPromises();
    expect(api.importTermius).toHaveBeenCalledWith('snapshot', ['host'], []);
    wrapper.unmount();
  });
  it('counts linked keys once and exposes them without requiring a separate selection', async () => {
    api.previewTermiusImport.mockResolvedValue({ ...preview, hosts: [{ ...preview.hosts[0], key_id: 'key' }] });
    const wrapper = mount(TermiusImportDrawer);
    await flushPromises();
    await wrapper.get('[data-test="termius-preview"]').trigger('click');
    await flushPromises();
    await wrapper.get('[data-test="termius-select-visible"]').setValue(true);
    expect(wrapper.get('[data-test="termius-selection-count"]').text()).toBe('1 hosts · 1 keys selected');
    await wrapper.get('[data-test="termius-keys-tab"]').trigger('click');
    expect(wrapper.text()).toContain('Included with host');
    await wrapper.get('[data-test="termius-select-visible"]').setValue(true);
    expect(wrapper.get('[data-test="termius-selection-count"]').text()).toBe('1 hosts · 1 keys selected');
    wrapper.unmount();
  });
  it('shows the proxy route and automatically counts required jump hosts and their keys', async () => {
    api.previewTermiusImport.mockResolvedValue({ ...preview, hosts: [
      { ...preview.hosts[0], jump_ids: ['hop'], proxy_label: 'HTTP · localhost:8080' },
      { ...preview.hosts[0], id: 'hop', name: 'bastion', key_id: 'key' },
    ] });
    const wrapper = mount(TermiusImportDrawer);
    await flushPromises();
    await wrapper.get('[data-test="termius-preview"]').trigger('click');
    await flushPromises();
    expect(wrapper.text()).toContain('HTTP · localhost:8080');
    expect(wrapper.text()).toContain('Jump hosts: bastion');
    await wrapper.get('[data-test="termius-host-host"]').setValue(true);
    expect(wrapper.text()).toContain('Included as jump host');
    expect(wrapper.get('[data-test="termius-selection-count"]').text()).toBe('2 hosts · 1 keys selected');
    await wrapper.get('[data-test="termius-confirm"]').trigger('click');
    await flushPromises();
    expect(api.importTermius).toHaveBeenCalledWith('snapshot', ['host'], []);
    wrapper.unmount();
  });
  it('combines status and search filters without losing hidden selections', async () => {
    const wrapper = mount(TermiusImportDrawer);
    await flushPromises();
    await wrapper.get('[data-test="termius-preview"]').trigger('click');
    await flushPromises();
    await wrapper.get('[data-test="termius-filter-available"]').trigger('click');
    expect(wrapper.find('[data-test="termius-host-proxy"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="termius-host-existing"]').exists()).toBe(false);
    await wrapper.get('[data-test="termius-select-visible"]').setValue(true);
    await wrapper.get('[data-test="termius-filter-blocked"]').trigger('click');
    expect(wrapper.find('[data-test="termius-host-host"]').exists()).toBe(false);
    expect(wrapper.get<HTMLInputElement>('[data-test="termius-host-proxy"]').element.disabled).toBe(true);
    expect(wrapper.get<HTMLButtonElement>('[data-test="termius-confirm"]').element.disabled).toBe(false);
    expect(wrapper.get<HTMLInputElement>('[data-test="termius-select-visible"]').element.disabled).toBe(true);
    await wrapper.get('[data-test="termius-filter-existing"]').trigger('click');
    expect(wrapper.find('[data-test="termius-host-proxy"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="termius-host-existing"]').exists()).toBe(true);
    await wrapper.get('[data-test="termius-search"]').setValue('not found');
    expect(wrapper.text()).toContain('No matching items');
    await wrapper.get('[data-test="termius-search"]').setValue('');
    await wrapper.get('[data-test="termius-filter-all"]').trigger('click');
    expect(wrapper.get<HTMLInputElement>('[data-test="termius-host-host"]').element.checked).toBe(true);
    await wrapper.get('[data-test="termius-confirm"]').trigger('click');
    await flushPromises();
    expect(api.importTermius).toHaveBeenCalledWith('snapshot', ['host'], []);
    wrapper.unmount();
  });
  it('requires choosing between multiple installations', async () => {
    api.discoverTermiusProfiles.mockResolvedValue(['/one', '/two']);
    const wrapper = mount(TermiusImportDrawer);
    await flushPromises();
    expect(wrapper.get<HTMLInputElement>('[data-test="termius-path"]').element.value).toBe('');
    expect(wrapper.get<HTMLButtonElement>('[data-test="termius-preview"]').element.disabled).toBe(true);
    wrapper.unmount();
  });
});
