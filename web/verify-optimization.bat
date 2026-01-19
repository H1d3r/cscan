@echo off
echo ========================================
echo 验证懒加载优化配置
echo ========================================
echo.

echo [1/4] 检查 AssetManagement.vue 是否使用懒加载...
findstr /C:"defineAsyncComponent" src\views\AssetManagement.vue >nul
if %errorlevel% equ 0 (
    echo ✓ AssetManagement.vue 已配置懒加载
) else (
    echo ✗ AssetManagement.vue 未找到懒加载配置
)
echo.

echo [2/4] 检查 Vite 配置是否包含代码分割...
findstr /C:"manualChunks" vite.config.js >nul
if %errorlevel% equ 0 (
    echo ✓ vite.config.js 已配置代码分割
) else (
    echo ✗ vite.config.js 未找到代码分割配置
)
echo.

echo [3/4] 检查性能监控工具是否存在...
if exist "src\utils\performance.js" (
    echo ✓ 性能监控工具已创建
) else (
    echo ✗ 性能监控工具不存在
)
echo.

echo [4/4] 检查文档是否完整...
set doc_count=0
if exist "PERFORMANCE_OPTIMIZATION.md" set /a doc_count+=1
if exist "test-lazy-loading.md" set /a doc_count+=1
if exist "OPTIMIZATION_SUMMARY.md" set /a doc_count+=1
echo ✓ 找到 %doc_count%/3 个文档文件
echo.

echo ========================================
echo 验证完成！
echo ========================================
echo.
echo 下一步操作：
echo 1. 运行 'npm install' 安装依赖（如果还没安装）
echo 2. 运行 'npm run dev' 启动开发服务器
echo 3. 访问 http://192.168.1.214:3000/asset-management
echo 4. 打开浏览器控制台查看性能数据
echo.
pause
