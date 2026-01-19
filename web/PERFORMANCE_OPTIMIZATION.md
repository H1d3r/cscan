# 前端性能优化说明

## 资产管理页面懒加载优化

### 优化内容

#### 1. 组件懒加载
- **位置**: `web/src/views/AssetManagement.vue`
- **改进**: 将三个子标签页组件（AssetGroupsTab、AssetInventoryTab、ScreenshotsTab）从静态导入改为动态懒加载
- **效果**: 
  - 初始页面加载时不会加载所有标签页的代码
  - 只在用户切换到对应标签页时才加载该组件
  - 减少首次加载的 JavaScript 体积

**优化前**:
```javascript
import AssetGroupsTab from './AssetManagement/AssetGroupsTab.vue'
import AssetInventoryTab from './AssetManagement/AssetInventoryTab.vue'
import ScreenshotsTab from './AssetManagement/ScreenshotsTab.vue'
```

**优化后**:
```javascript
const AssetGroupsTab = defineAsyncComponent(() => 
  import('./AssetManagement/AssetGroupsTab.vue')
)
const AssetInventoryTab = defineAsyncComponent(() => 
  import('./AssetManagement/AssetInventoryTab.vue')
)
const ScreenshotsTab = defineAsyncComponent(() => 
  import('./AssetManagement/ScreenshotsTab.vue')
)
```

#### 2. 条件渲染优化
- 使用 `v-if` 配合 `activeTab` 确保只渲染当前激活的标签页
- 添加 `Suspense` 组件提供加载状态反馈
- 添加加载动画提升用户体验

#### 3. Vite 构建优化
- **位置**: `web/vite.config.js`
- **改进**:
  - 配置代码分割策略，将第三方库分离打包
  - Vue 核心库（vue、vue-router、pinia）单独打包
  - Element Plus 及图标库单独打包
  - 其他工具库（axios、dayjs、echarts）单独打包
  - 启用 CSS 代码分割
  - 生产环境移除 console 和 debugger

### 性能提升预期

1. **首次加载速度**: 减少 30-50% 的初始 JavaScript 加载量
2. **交互响应**: 标签页切换时按需加载，首次切换可能有轻微延迟（100-300ms）
3. **缓存效率**: 代码分割后，更新代码时用户只需重新下载变更的模块
4. **内存占用**: 减少不必要的组件实例化，降低内存使用

### 使用建议

1. **开发环境**: 
   ```bash
   cd web
   npm run dev
   ```
   访问 http://localhost:3000/asset-management 测试

2. **生产构建**:
   ```bash
   cd web
   npm run build
   ```
   构建后会在 `dist` 目录生成优化后的文件

3. **查看构建分析**:
   可以添加 `rollup-plugin-visualizer` 插件查看打包体积分布

### 后续优化建议

1. **图片懒加载**: 如果截图清单有大量图片，建议添加图片懒加载
2. **虚拟滚动**: 如果表格数据量很大，可以考虑使用虚拟滚动
3. **预加载**: 可以在用户悬停标签页时预加载对应组件
4. **Service Worker**: 添加 PWA 支持，实现离线缓存

### 监控指标

建议关注以下性能指标：
- **FCP (First Contentful Paint)**: 首次内容绘制时间
- **LCP (Largest Contentful Paint)**: 最大内容绘制时间
- **TTI (Time to Interactive)**: 可交互时间
- **Bundle Size**: 打包体积大小

可以使用 Chrome DevTools 的 Lighthouse 进行性能评估。
