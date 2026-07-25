# Sidebar 搜索框收起为图标 — Design

## 背景

`TaskSidebar` 展开态头部当前布局：

```
[会…] [<input type=search 搜索……………………………………………………………………>] [按主机] [«]
```

搜索输入使用 `flex: 1 1 auto; min-width: 60px;` 抢占了大部分横向空间，导致：

- `.title`（"会话"）被 `text-overflow: ellipsis` 截成一两个字
- 常态下（用户没在搜索）这段空间几乎不发挥价值

## 目标

搜索区域默认只占一个图标位；点击图标或按 `Cmd+F` 才展开成输入框。

不改：

- 移动端 `MobileSessionList`（不同 UI）
- 分组切换按钮 `.group-toggle`
- 折叠按钮 `.collapse-button`
- 收起态（rail）— 本 spec 只涉及 `.expanded` 头部

## 头部两种状态

### 收起态（默认）

```
[会话完整标题…………………………………] [🔍] [按主机] [«]
```

- 用 `<button class="search-icon-btn">` 内联一个 16×16 放大镜 SVG（仓库其他组件如 `WindowControls.vue`、`TitleBar.vue` 都用 inline SVG，不引外部图标库；ASCII 图里的 🔍 只是占位说明）
- `.title` 保持现有类名与样式，因空间释放会自然占满
- 图标按钮尺寸对齐现有 `.group-toggle` / `.collapse-button` 的视觉重量

### 展开态

```
[<input type=search autofocus 展开…………………………>] [按主机] [«]
```

- 展开时 `.title` 隐藏（`v-if="!searchOpen"`）
- 输入沿用现有 `.sidebar-search` 样式与 `type="search"` 原生 × 清除键
- 分组按钮和折叠按钮位置不变

## 状态切换

**收起 → 展开**

- 点击 🔍 图标按钮
- App 侧调用 `taskSidebarRef.focusSearch()`（`Cmd+F` 快捷键触发）
- `query` 非空时保底：即使外部把 `searchOpen` 弄成 false，只要 `query` 有值就保持展开（`v-if="searchOpen || query"`）

**展开 → 收起**

- 按 `Esc` 且 `query` 为空 → 收起
- 输入 `blur` 事件且 `query` 为空 → 收起
- `query` 非空时上述两条不生效（避免有搜索结果时误折叠）

## 实现要点

`desktop/frontend/src/components/TaskSidebar.vue`：

1. 新增本地状态：`const searchOpen = ref(false)`
2. 修改 header 模板：
   ```html
   <span v-if="!searchOpen" class="title">{{ ... }}</span>
   <button
     v-if="!(searchOpen || query)"
     class="search-icon-btn"
     data-test="sidebar-search-toggle"
     :title="t('tasks.sidebar.searchPlaceholder')"
     :aria-label="t('tasks.sidebar.searchPlaceholder')"
     @click="openSearch"
   >
     <!-- 放大镜 SVG -->
   </button>
   <input
     v-else
     ref="searchEl"
     v-model="query"
     type="search"
     class="sidebar-search"
     ...
     @keydown.esc.prevent="onSearchEsc"
     @blur="onSearchBlur"
   />
   ```
3. `openSearch()`：`searchOpen = true`，`nextTick` 后 `searchEl.value?.focus() + select()`
4. `onSearchEsc()`：若 `query` 为空则 `searchOpen = false`；否则清空 query 保持展开（沿用原语义可选，本 spec 采纳"空则收起，非空则清空并保持展开"）
   - 决策：**空则收起；非空则先清空 query（沿用现有 esc 语义），下一次再按 esc 才收起**。这样单次 esc 只做一件事，符合直觉。
5. `onSearchBlur()`：若 `query` 为空则 `searchOpen = false`
6. `focusSearch()` (defineExpose)：内部先 `searchOpen = true`，`nextTick` 后 focus + select
7. CSS：新增 `.search-icon-btn` 样式，视觉参照 `.collapse-button`（透明背景、无边框、`cursor: pointer`、`font-size: 14px`）；`.title` 的 `flex` 改成 `1 1 auto` 让它占满空间（当前是 `0 1 auto`）

## 测试

在 `desktop/frontend/src/components/TaskSidebar.test.ts` 新增：

- **默认渲染图标而非输入**：初次挂载时 `[data-test="sidebar-search-toggle"]` 存在，`[data-test="sidebar-search"]` 不存在
- **点击图标展开并聚焦**：click toggle 后 `sidebar-search` 出现且 `document.activeElement === input`（用 `flushPromises` + `nextTick`）
- **空值 Esc 收起**：展开态下 keydown Esc（无 query）→ 输入消失，图标出现
- **非空 Esc 只清空**：query 非空时 Esc 只清 query，输入仍在
- **失焦 + 空值收起**：blur 且 query 为空 → 收起
- **失焦 + 非空保持展开**：blur 且 query 非空 → 不收起
- **`focusSearch()` 收起态也能展开**：直接调用组件 exposed 方法，输入出现且获焦
- **`query` 非空持续存在**：外部 mock 让 query 有值时，即使 `searchOpen=false`，输入仍展示

`data-test="sidebar-search"` 保持不变，避免破坏可能存在的下游断言。

## 不做的事（YAGNI）

- 不加展开/收起过渡动画
- 不加显式 × 关闭按钮
- 不动其他任何组件
- 不改 i18n（复用现有 `searchPlaceholder`）
