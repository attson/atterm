<script setup>
import { computed } from 'vue'
import { withBase } from 'vitepress'

const props = defineProps({
  locale: { type: String, default: 'zh' },
})

const releaseBase = 'https://github.com/attson/atterm/releases/latest/download'
const releases = 'https://github.com/attson/atterm/releases/latest'
const sourceGuide = computed(() => withBase(props.locale === 'en' ? '/en/guide/' : '/guide/'))
const copy = computed(() => props.locale === 'en' ? {
  mac: 'macOS',
  macHint: 'Choose Apple Silicon (arm64) or Intel (amd64).',
  macWarning: 'Current macOS builds are unsigned and not notarized. Gatekeeper requires removing the quarantine attribute after installation.',
  linux: 'Linux',
  linuxHint: 'Portable tarballs for x64 and arm64. Versioned .deb packages are available on Releases.',
  windows: 'Windows',
  windowsHint: 'Windows x64 ZIP. Windows 10 may require the WebView2 runtime.',
  ios: 'iPhone / iPad',
  iosHint: 'There is no public App Store or TestFlight build yet. Build the Capacitor app from source.',
  source: 'Source build guide',
  all: 'All release assets',
  verify: 'Verify downloads',
  verifyHint: 'Download the signed checksum manifest before installing.',
} : {
  mac: 'macOS',
  macHint: 'Apple Silicon 选 arm64，Intel 选 amd64。',
  macWarning: '当前 macOS 构件未签名、未公证，安装后需按说明移除 Gatekeeper 隔离属性。',
  linux: 'Linux',
  linuxHint: '提供 x64 与 arm64 便携 tar 包；版本化 .deb 请到 Releases 选择。',
  windows: 'Windows',
  windowsHint: '提供 Windows x64 ZIP；Windows 10 可能需要另装 WebView2。',
  ios: 'iPhone / iPad',
  iosHint: '目前没有公开 App Store 或 TestFlight 构件，需要从源码构建 Capacitor app。',
  source: '源码构建说明',
  all: '查看全部构件',
  verify: '校验下载',
  verifyHint: '安装前下载已签名的 checksum 清单进行校验。',
})

const asset = (name) => `${releaseBase}/${name}`
</script>

<template>
  <div class="release-downloads">
    <section class="release-platform">
      <div class="release-platform-copy">
        <h3>{{ copy.mac }}</h3>
        <p>{{ copy.macHint }}</p>
        <p class="release-warning">{{ copy.macWarning }}</p>
      </div>
      <div class="release-links">
        <a data-testid="download-macos-arm64" :href="asset('AT-Term-darwin-arm64.zip')">Apple Silicon</a>
        <a :href="asset('AT-Term-darwin-amd64.zip')">Intel</a>
      </div>
    </section>

    <section class="release-platform">
      <div class="release-platform-copy">
        <h3>{{ copy.linux }}</h3>
        <p>{{ copy.linuxHint }}</p>
      </div>
      <div class="release-links">
        <a data-testid="download-linux-amd64" :href="asset('AT-Term-linux-amd64.tar.gz')">x64</a>
        <a :href="asset('AT-Term-linux-arm64.tar.gz')">arm64</a>
      </div>
    </section>

    <section class="release-platform">
      <div class="release-platform-copy">
        <h3>{{ copy.windows }}</h3>
        <p>{{ copy.windowsHint }}</p>
      </div>
      <div class="release-links">
        <a data-testid="download-windows-amd64" :href="asset('AT-Term-windows-amd64.zip')">Windows x64</a>
      </div>
    </section>

    <section class="release-platform">
      <div class="release-platform-copy">
        <h3>{{ copy.ios }}</h3>
        <p>{{ copy.iosHint }}</p>
      </div>
      <div class="release-links">
        <a :href="sourceGuide">{{ copy.source }}</a>
      </div>
    </section>

    <div class="release-verify">
      <div><strong>{{ copy.verify }}</strong><span>{{ copy.verifyHint }}</span></div>
      <div class="release-links">
        <a data-testid="download-checksums" :href="asset('SHA256SUMS')">SHA256SUMS</a>
        <a :href="asset('SHA256SUMS.sig')">SHA256SUMS.sig</a>
        <a :href="releases">{{ copy.all }}</a>
      </div>
    </div>
  </div>
</template>
