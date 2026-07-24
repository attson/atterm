# 会话侧栏搜索 — design

Date: 2026-07-24
Status: Drafted — awaiting user review before plan.

## 0. Summary

左侧会话栏(`TaskSidebar` → `TaskGroupedList`)现在按 host 或 state 分组显示
所有 relay-attached sessions。用户在 20+ 会话时寻找特定任务只能靠肉眼扫和
折叠/展开分组,交互成本高。

本设计在侧栏 header 内联加一条搜索输入框:输入子串,即时按
`title` / `cwd` / `current_command` 匹配,命中之外的行**隐藏**,含命中行
的折叠组自动展开;query 清空后列表回到原状态。搜索是临时定位工具,不落
config、不跨重启保留;快捷键 `Cmd/Ctrl+F` focus 输入框,`Esc` 清空。

## 1. Goals

- 侧栏 header 常驻搜索框:内联在 `会话` 标题右侧,不占额外一行高度。
- 匹配字段:`title` + `cwd` + `current_command`,子串 case-insensitive。
- 命中过滤:非命中行隐藏,含命中行的折叠组临时展开;整组空隐藏。
- 空态提示:所有 group + pinned 均为空时渲染"没有匹配 <q> 的会话"。
- 快捷键 `Cmd/Ctrl+F`(通过现有 `useTerminalShortcuts` 注册的
  `sidebar.focus-search` action):focus 搜索输入框;侧栏折叠时先展开再
  focus。输入框内 `Esc` 清空 query 但保留 focus。
- 实现限定在 `desktop/frontend/`;不涉及协议、relay、mobile、web。

## 2. Non-goals

- **不跨端**:mobile / web 不加搜索;若他们要,另立需求。
- **不做模糊匹配 / 正则 / 多 token AND**:子串精确子串;YAGNI。
- **不做搜索历史 / recent queries**:临时工具,不留痕。
- **不落 config**:每次开 app query 从空开始;不像 `groupBy` 那种是偏好。
- **不做 debounce**:列表规模常在几十条,`includes` 每 keystroke 亚毫秒;
  加 debounce 只带输入延迟感。
- **不豁免 pin**:置顶会话也参与过滤(即 `pinnedSessions` 也 `.filter(matches)`);
  用户搜索时想看什么就是什么,pin 是长期定位、搜索是即时定位,后者优先。
- **不做搜索命中高亮 (在 title 里加 `<mark>`)**:控件已够满足"我看到了它就在"的
  需求,加高亮不成本比。若后来觉得需要再补。
- **Rail (折叠 sidebar) 不显示搜索 UI**:折叠状态是纯图标 rail,`railIcons`
  仍按 urgency 排前 20;query 状态保留,展开后仍在(不主动清)。

## 3. 现状

- `desktop/frontend/src/components/TaskSidebar.vue:169-189` — sidebar-header
  当前是 `会话 title + group-toggle + collapse-button`,`title` 用 `flex: 1`
  占中间。要加搜索框需要改成 `title` 收缩、search input 占 flex-grow。
- `desktop/frontend/src/components/TaskGroupedList.vue:75-102` — 已有
  `groups` / `filteredGroups` / `groupKeys` / `pinnedSessions` computed +
  `collapsedGroups` Set;搜索层加在这些基础上叠一次 filter 即可。
- `desktop/frontend/src/composables/useTerminalShortcuts.ts` +
  `lib/shortcutBindings.ts` — action registry。已有 `toggleTaskSidebar`
  (`Mod+KeyB`)、pane / tab 一堆快捷键。新增 `sidebar.focus-search` 走同一
  套路。
- `desktop/frontend/src/i18n/messages/{en,zh}.ts` — 新增
  `tasks.sidebar.searchPlaceholder` / `tasks.search.empty` 两条 key。

`RemoteSession` 字段(`platform/types.ts:82-108`):`title`、`cwd?`、
`current_command?` 都是可空 string。`session_id` 是 UUID(不匹配)、
`host_id` 是 hex(不匹配)、`type` 是分类枚举(不匹配)。

## 4. 设计

### 4.1 Header 布局

`TaskSidebar.vue::.sidebar-header` 改成:

```
[会话] [🔍 <input>, flex:1] [按主机] [«]
```

- `.title` 从 `flex: 1` 改成 `flex: 0 0 auto`(仅字宽,~30px)。
- 新增 `.sidebar-search`:`<input type="search">`,`flex: 1 1 auto`,
  `min-width: 60px`,`ref="searchEl"`,`v-model="query"`。
- placeholder = `t('tasks.sidebar.searchPlaceholder')` (中: `搜索`,
  英: `Search`)。
- 样式:
  - 默认 `border: 1px solid transparent; background: rgba(255,255,255,0.03)`。
  - focus 态:`border-color: var(--border)`,背景不变。
  - 高度和 `.group-toggle`(padding: 1px 6px)对齐,行高 ~20px。
  - `type="search"` 自带 × 清除按钮(Chromium/WebKit 默认渲染),不额外做。
- 侧栏 min-width 180px:`会话`≈30 + input≥60 + `按主机`≈40 + `«`≈16 + gaps
  ≈ 160-170px,能放下(有余量的话 input 就占多)。侧栏拖到极限时输入框
  被压到 60px 仍可用(placeholder 变省略,输入时能左右滚动)。

### 4.2 State + 数据流

- `TaskSidebar.vue`:`const query = ref("")`;通过 `:search-query="query"`
  prop 传给 `TaskGroupedList`。
- `TaskGroupedList.vue`:接收 `searchQuery: string` prop
  (`withDefaults({ searchQuery: "" })`),内部
  `const q = computed(() => props.searchQuery.trim().toLocaleLowerCase())`。

Query 生命周期:

| 事件 | query 状态 |
| --- | --- |
| App 启动 | `""` |
| 用户输入 | 实时更新 |
| `Esc` (输入框内 focus) | 清空 `""`, focus 保留 |
| 侧栏折叠 (`collapsed = true`) | 保留 (rail 不显示 UI,但不清 state) |
| 侧栏展开 | 保留 (input 重挂载时 `v-model` 读回 ref) |
| App 关闭/重启 | 丢失 (不落 config) |

### 4.3 匹配逻辑

新增纯函数文件 `desktop/frontend/src/lib/sessionMatch.ts`:

```ts
import type { RemoteSession } from "../platform/types";

// matchesSession returns true when q is empty (no filter) or when q is a
// case-insensitive substring of any of the session's user-visible identity
// fields (title / cwd / current_command). Fields are matched independently;
// a match in any one wins. q must already be trimmed + lowercased by the
// caller — this keeps the hot path (called per session per keystroke) branch-
// free of allocation. Empty / null fields never contribute.
export function matchesSession(s: RemoteSession, q: string): boolean {
  if (!q) return true;
  const t = s.title;
  if (t && t.toLocaleLowerCase().includes(q)) return true;
  const c = s.cwd;
  if (c && c.toLocaleLowerCase().includes(q)) return true;
  const cc = s.current_command;
  if (cc && cc.toLocaleLowerCase().includes(q)) return true;
  return false;
}
```

Callers:`TaskGroupedList.vue`:

```ts
import { matchesSession } from "../lib/sessionMatch";

const q = computed(() => (props.searchQuery ?? "").trim().toLocaleLowerCase());

const filteredGroups = computed<Record<string, RemoteSession[]>>(() => {
  const out: Record<string, RemoteSession[]> = {};
  for (const [k, list] of Object.entries(groups.value)) {
    out[k] = list.filter(
      (s) => !pins.isPinned(s.session_id) && matchesSession(s, q.value),
    );
  }
  return out;
});

// pinnedSessions computed 也叠一层 .filter((s) => matchesSession(s, q.value))
```

`groupKeys` 的空组过滤条件从 `(groups.value[key] ?? []).length > 0` 改成
`(filteredGroups.value[key] ?? []).length > 0`,自动 hide 空 host / state。

`pinnedSessions.length === 0` 时 pinned 组的整个 header + 行块也隐藏
(在 template `v-if="pinnedSessions.length > 0"` 已经这么做的,不用改)。

### 4.4 折叠组自动展开

`isGroupCollapsed(key)` 加短路:

```ts
function isGroupCollapsed(key: string): boolean {
  if (q.value) return false;                    // 搜索中所有组视为展开
  return collapsedGroups.value.has(key);
}
```

真实 `collapsedGroups` state 不被修改;query 清空后 `isGroupCollapsed` 回到
真值,原折叠状态恢复。

### 4.5 空态

`TaskGroupedList` template 底部(在 groups 循环之后)追加:

```vue
<div
  v-if="q && !hasAnyMatch"
  class="empty-hint"
  data-test="search-empty"
>
  {{ t('tasks.search.empty', { q: props.searchQuery }) }}
</div>
```

`hasAnyMatch = computed(() => pinnedSessions.value.length > 0 || groupKeys.value.length > 0)`。

i18n:
- `zh`: `搜索"{q}" 无匹配会话`
- `en`: `No sessions match "{q}"`

样式:居中,灰字,padding: 24px 12px;和 sidebar 其它空态一致。

### 4.6 快捷键

`lib/shortcutBindings.ts::ACTIONS` 追加一条:

```ts
{ id: "sidebar.focus-search", group: "sidebar", labelKey: "settings.shortcuts.focusSidebarSearch", defaultBinding: "Mod+KeyF" },
```

`useTerminalShortcuts.ts`:
- `ShortcutHandlers` 接口加 `onFocusSidebarSearch?: () => void`。
- `dispatch` 加 `case "sidebar.focus-search": h.onFocusSidebarSearch?.(); return true;`。

`App.vue` 传递 handler:
- 侧栏折叠时:先 `setTaskSidebarCollapsed(false)`(或直接设 ref),`nextTick` 后再 focus。
- 展开时:直接 focus。
- 通过 template ref `<TaskSidebar ref="taskSidebarRef" ...>` + expose 的
  `focusSearch()` 方法调用:

```ts
// TaskSidebar.vue
const searchEl = ref<HTMLInputElement | null>(null);
defineExpose({
  async focusSearch() {
    if (props.collapsed) {
      emit("update:collapsed", false);
      await nextTick();
    }
    searchEl.value?.focus();
    searchEl.value?.select();
  },
});
```

App.vue:`onFocusSidebarSearch: () => taskSidebarRef.value?.focusSearch()`。

`Esc` 在输入框内:

```vue
<input
  ref="searchEl"
  v-model="query"
  @keydown.esc.prevent="query = ''"
  ...
/>
```

`.esc.prevent` 阻止全局 keydown(避免触发别的 Esc handler),不 blur。

### 4.7 i18n 增量

`desktop/frontend/src/i18n/messages/zh.ts`:
```ts
tasks: {
  ...
  sidebar: {
    ...
    searchPlaceholder: "搜索",
    ...
  },
  search: {
    empty: '搜索 "{q}" 无匹配会话',
  },
},
settings: {
  shortcuts: {
    ...
    focusSidebarSearch: "聚焦侧栏搜索",
  },
},
```

`en.ts` 对应:
```ts
searchPlaceholder: "Search",
empty: 'No sessions match "{q}"',
focusSidebarSearch: "Focus sidebar search",
```

### 4.8 边界

| 情形 | 行为 |
| --- | --- |
| query 含正则元字符 `.`, `*`, `[]` | 字面处理 (`includes`),无 escape 顾虑 |
| query 含 emoji | UTF-16 `includes` 正确匹配 |
| query 前后有空白 | `.trim()` 消掉;`" abc "` = `"abc"` |
| query 内部空格 (如 `"proj web"`) | 视为字面,子串连着出现才算命中 |
| 会话 title / cwd / current_command 空 | 该字段不参与匹配,其他字段仍试 |
| 命中的 session 被 close | 从命中集自动消失,列表实时收缩 |
| 命中集变空 | 走空态提示 |
| 搜索中 pin / unpin | 集合刷新,搜索继续生效 |
| current_command 随 spinner tick 变 | 命中忽有忽无,属"实时匹配"合理行为,不做平滑 |
| 侧栏拖极窄 (180px) | 输入框压到 ~60px 仍可用,placeholder 截断 |
| Cmd+F 冲突 (webview 内浏览器 find) | `useTerminalShortcuts` 用 capture-phase 抢,`preventDefault` 覆盖 |
| 用户改了 `sidebar.focus-search` 到别的 chord | 走同一 shortcut registry,自动生效 |

## 5. Test plan

**新增/扩展:**

- `desktop/frontend/src/lib/sessionMatch.test.ts` (新)
  - 空 query 全通过
  - `title` / `cwd` / `current_command` 三分支各自命中
  - Case-insensitive:`"ABC"` matches `"abc"` / `"XYZ"`
  - CJK 命中:`"支付"` matches `title="支付网关"`
  - 空字段不 crash
  - query 带空格视为字面
  - 空 title + 空 cwd + 空 current_command:任何 query 都不命中
- `TaskSidebar.test.ts`
  - 搜索输入框可见性:`collapsed=false` 可见,`collapsed=true`(rail)不可见
  - 输入 emits / 反映到 `TaskGroupedList` prop
  - `Esc` 清空 query 但保留 focus
  - `focusSearch()` 展开侧栏并 focus 输入框(mock `collapsed = true → false`)
- `TaskGroupedList.test.ts`
  - `searchQuery` prop 命中 → 只显示命中行
  - 命中在折叠组内 → 该组自动展开
  - 全 group 空 → 空态提示带 query 值
  - pin 会话参与过滤(命中显示,不命中不豁免)
  - query 清空 → 恢复原折叠 + 全量显示
- `useTerminalShortcuts.test.ts`
  - `Mod+KeyF` route 到 `sidebar.focus-search` action
  - `onFocusSidebarSearch` 被调用,`preventDefault` + `stopPropagation`

**手测清单**(plan 阶段展开):

- 30 个 session,输入 `proj` 命中 3 个 → 只剩 3 行,各组 header 带 count 未变(count 仍是"全量")
- 折叠 host X,搜索命中 X 内一行 → X 自动展开;清空 query → X 回到折叠
- Cmd+F 反复触发不 leak keydown listener(devtools performance 看不到累计)
- 侧栏拖到 180px:所有 header 元素齐整,输入框可点、可输入
- CJK 搜索 `支付` 命中中文 title
- 命中的 session close → 命中集自动收缩;命中集变空 → 空态提示

## 6. Rollout notes

- 无 wire / config 迁移;纯前端。
- i18n 新增 3 条 key(zh + en 各 3):`tasks.sidebar.searchPlaceholder` /
  `tasks.search.empty` / `settings.shortcuts.focusSidebarSearch`。
- Shortcut registry 加一条 `sidebar.focus-search`;老用户如果 override 过
  `Mod+KeyF` 到别的 action(不太可能——`KeyF` 目前空闲)会看到 conflict
  提示(现有 `conflictsWith` 机制处理)。
- 无向后兼容担忧(single-user project;memory `feedback_no_backward_compat.md`)。
- 不影响 mobile / web。发布走 v0.3-dev → 下一个 v0.3.x tag。

## 7. Open questions

_None._ 关键决策已在 brainstorm 敲定:

- UX:侧栏 header 常驻内联搜索框(不弹 palette、不 toggle 图标)
- 匹配字段:title + cwd + current_command
- 过滤:隐藏不命中,折叠组自动展开,pin 也参与过滤
- 匹配算法:子串 case-insensitive,无 debounce / 无正则 / 无 fuzzy
- 状态:纯本地,不落 config,`Esc` 清空
- 快捷键:`Cmd/Ctrl+F` 进 shortcut registry
