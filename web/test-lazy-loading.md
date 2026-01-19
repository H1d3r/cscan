# 懒加载效果测试指南

## 测试步骤

### 1. 启动开发服务器
```bash
cd web
npm run dev
```

### 2. 打开浏览器开发者工具
1. 访问 http://192.168.1.214:3000/asset-management
2. 按 F12 打开开发者工具
3. 切换到 "Network" (网络) 标签页
4. 勾选 "Disable cache" (禁用缓存)

### 3. 观察懒加载效果

#### 测试场景 1: 首次加载
1. 刷新页面 (Ctrl+F5)
2. 观察 Network 面板中加载的 JS 文件
3. **预期结果**: 
   - 只加载主应用代码和当前标签页（资产分组）的代码
   - 不会加载其他两个标签页的代码
   - 文件名类似: `AssetGroupsTab-[hash].js`

#### 测试场景 2: 切换标签页
1. 点击 "资产清单" 标签
2. 观察 Network 面板
3. **预期结果**:
   - 会动态加载 `AssetInventoryTab-[hash].js`
   - 显示加载动画（旋转的图标和"加载中..."文字）
   - 加载完成后显示内容

4. 点击 "截图清单" 标签
5. **预期结果**:
   - 会动态加载 `ScreenshotsTab-[hash].js`

#### 测试场景 3: 再次切换
1. 切换回 "资产分组" 标签
2. **预期结果**:
   - 不会重新加载代码（已缓存）
   - 立即显示内容，无加载延迟

### 4. 性能对比

#### 优化前的加载情况
- 首次加载会下载所有三个标签页的代码
- 初始 JS 体积较大
- 用户可能永远不会访问某些标签页，但代码已经下载

#### 优化后的加载情况
- 首次只加载当前标签页代码
- 按需加载其他标签页
- 减少初始加载时间和带宽消耗

### 5. 使用 Chrome Lighthouse 测试

1. 打开开发者工具
2. 切换到 "Lighthouse" 标签
3. 选择 "Performance" 类别
4. 点击 "Analyze page load"
5. 查看性能评分和建议

**关注指标**:
- Performance Score (性能评分)
- First Contentful Paint (首次内容绘制)
- Largest Contentful Paint (最大内容绘制)
- Total Blocking Time (总阻塞时间)
- Speed Index (速度指数)

### 6. 生产构建测试

```bash
cd web
npm run build
```

查看 `dist` 目录:
- 检查是否生成了多个独立的 chunk 文件
- 验证代码分割是否生效
- 查看各个文件的大小

**预期文件结构**:
```
dist/
├── js/
│   ├── index-[hash].js          # 主入口
│   ├── vue-vendor-[hash].js     # Vue 核心库
│   ├── element-plus-[hash].js   # Element Plus
│   ├── vendor-[hash].js         # 其他第三方库
│   ├── AssetGroupsTab-[hash].js
│   ├── AssetInventoryTab-[hash].js
│   └── ScreenshotsTab-[hash].js
└── css/
    └── ...
```

## 性能优化验证清单

- [ ] 首次加载时只下载必要的代码
- [ ] 切换标签页时动态加载对应组件
- [ ] 显示加载动画提供用户反馈
- [ ] 已加载的组件不会重复下载
- [ ] 生产构建生成了独立的 chunk 文件
- [ ] 第三方库被正确分离打包
- [ ] 总体 JS 体积减小

## 故障排查

### 问题: 懒加载不生效
**解决方案**:
1. 确认使用了 `defineAsyncComponent`
2. 检查 Vite 配置中的 `build.rollupOptions`
3. 清除缓存后重新构建: `rm -rf node_modules/.vite && npm run dev`

### 问题: 加载动画不显示
**解决方案**:
1. 确认使用了 `<Suspense>` 组件
2. 检查 Loading 图标是否正确导入
3. 验证 CSS 样式是否正确应用

### 问题: 切换标签页有明显延迟
**解决方案**:
1. 这是正常现象（首次加载组件）
2. 可以考虑添加预加载策略
3. 优化组件内部的数据加载逻辑
