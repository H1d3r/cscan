/**
 * 分析打包文件大小的脚本
 * 运行: node analyze-bundle.js
 */

import fs from 'fs'
import path from 'path'
import { fileURLToPath } from 'url'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

// 递归获取目录下所有文件
function getAllFiles(dirPath, arrayOfFiles = []) {
  const files = fs.readdirSync(dirPath)

  files.forEach(file => {
    const filePath = path.join(dirPath, file)
    if (fs.statSync(filePath).isDirectory()) {
      arrayOfFiles = getAllFiles(filePath, arrayOfFiles)
    } else {
      arrayOfFiles.push(filePath)
    }
  })

  return arrayOfFiles
}

// 格式化文件大小
function formatSize(bytes) {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(2) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(2) + ' MB'
}

// 分析源代码文件
function analyzeSourceFiles() {
  console.log('\n📊 源代码文件分析\n')
  console.log('=' .repeat(80))
  
  const srcDir = path.join(__dirname, 'src')
  const files = getAllFiles(srcDir)
  
  const vueFiles = files
    .filter(f => f.endsWith('.vue'))
    .map(f => {
      const stats = fs.statSync(f)
      const content = fs.readFileSync(f, 'utf-8')
      const lines = content.split('\n').length
      return {
        path: path.relative(srcDir, f),
        size: stats.size,
        lines: lines
      }
    })
    .sort((a, b) => b.size - a.size)
  
  console.log('\n🔍 最大的 Vue 组件文件:\n')
  vueFiles.slice(0, 10).forEach((file, index) => {
    const sizeStr = formatSize(file.size).padEnd(12)
    const linesStr = String(file.lines).padStart(5)
    console.log(`${index + 1}. ${sizeStr} ${linesStr} 行  ${file.path}`)
  })
  
  // 统计
  const totalSize = vueFiles.reduce((sum, f) => sum + f.size, 0)
  const totalLines = vueFiles.reduce((sum, f) => sum + f.lines, 0)
  
  console.log('\n📈 统计信息:')
  console.log(`   总文件数: ${vueFiles.length}`)
  console.log(`   总大小: ${formatSize(totalSize)}`)
  console.log(`   总行数: ${totalLines.toLocaleString()}`)
  console.log(`   平均大小: ${formatSize(totalSize / vueFiles.length)}`)
  console.log(`   平均行数: ${Math.round(totalLines / vueFiles.length)}`)
  
  // 找出超大文件
  const largeFiles = vueFiles.filter(f => f.size > 30 * 1024 || f.lines > 1000)
  if (largeFiles.length > 0) {
    console.log('\n⚠️  需要优化的大文件:')
    largeFiles.forEach(file => {
      console.log(`   ${file.path}`)
      console.log(`   └─ ${formatSize(file.size)}, ${file.lines} 行`)
      if (file.size > 50 * 1024) {
        console.log(`      🔴 严重: 文件过大，建议拆分`)
      } else if (file.size > 30 * 1024) {
        console.log(`      🟡 警告: 文件较大，考虑优化`)
      }
    })
  }
}

// 分析构建产物
function analyzeBuildFiles() {
  const distDir = path.join(__dirname, 'dist')
  
  if (!fs.existsSync(distDir)) {
    console.log('\n⚠️  dist 目录不存在，请先运行 npm run build')
    return
  }
  
  console.log('\n\n📦 构建产物分析\n')
  console.log('=' .repeat(80))
  
  const files = getAllFiles(distDir)
  
  // JS 文件
  const jsFiles = files
    .filter(f => f.endsWith('.js'))
    .map(f => ({
      path: path.relative(distDir, f),
      size: fs.statSync(f).size
    }))
    .sort((a, b) => b.size - a.size)
  
  console.log('\n📜 JavaScript 文件:\n')
  jsFiles.slice(0, 10).forEach((file, index) => {
    const sizeStr = formatSize(file.size).padEnd(12)
    console.log(`${index + 1}. ${sizeStr} ${file.path}`)
  })
  
  // CSS 文件
  const cssFiles = files
    .filter(f => f.endsWith('.css'))
    .map(f => ({
      path: path.relative(distDir, f),
      size: fs.statSync(f).size
    }))
    .sort((a, b) => b.size - a.size)
  
  if (cssFiles.length > 0) {
    console.log('\n🎨 CSS 文件:\n')
    cssFiles.forEach((file, index) => {
      const sizeStr = formatSize(file.size).padEnd(12)
      console.log(`${index + 1}. ${sizeStr} ${file.path}`)
    })
  }
  
  // 图片文件
  const imageFiles = files
    .filter(f => /\.(png|jpg|jpeg|gif|svg|webp|ico)$/i.test(f))
    .map(f => ({
      path: path.relative(distDir, f),
      size: fs.statSync(f).size
    }))
    .sort((a, b) => b.size - a.size)
  
  if (imageFiles.length > 0) {
    console.log('\n🖼️  图片文件:\n')
    imageFiles.slice(0, 5).forEach((file, index) => {
      const sizeStr = formatSize(file.size).padEnd(12)
      console.log(`${index + 1}. ${sizeStr} ${file.path}`)
    })
  }
  
  // 总体统计
  const totalJsSize = jsFiles.reduce((sum, f) => sum + f.size, 0)
  const totalCssSize = cssFiles.reduce((sum, f) => sum + f.size, 0)
  const totalImageSize = imageFiles.reduce((sum, f) => sum + f.size, 0)
  const totalSize = totalJsSize + totalCssSize + totalImageSize
  
  console.log('\n📊 构建产物统计:')
  console.log(`   JavaScript: ${formatSize(totalJsSize)} (${jsFiles.length} 个文件)`)
  console.log(`   CSS: ${formatSize(totalCssSize)} (${cssFiles.length} 个文件)`)
  console.log(`   图片: ${formatSize(totalImageSize)} (${imageFiles.length} 个文件)`)
  console.log(`   总计: ${formatSize(totalSize)}`)
  
  // 性能建议
  console.log('\n💡 优化建议:')
  if (totalJsSize > 1024 * 1024) {
    console.log('   🔴 JavaScript 总大小超过 1MB，建议:')
    console.log('      - 检查是否有未使用的依赖')
    console.log('      - 使用代码分割')
    console.log('      - 启用 Tree Shaking')
  }
  if (jsFiles.some(f => f.size > 500 * 1024)) {
    console.log('   🟡 存在超过 500KB 的 JS 文件，建议拆分')
  }
  if (totalImageSize > 2 * 1024 * 1024) {
    console.log('   🟡 图片总大小超过 2MB，建议:')
    console.log('      - 压缩图片')
    console.log('      - 使用 WebP 格式')
    console.log('      - 使用 CDN')
  }
}

// 主函数
function main() {
  console.log('\n🔍 CScan Web 性能分析工具\n')
  
  try {
    analyzeSourceFiles()
    analyzeBuildFiles()
    
    console.log('\n' + '='.repeat(80))
    console.log('\n✅ 分析完成！\n')
    console.log('📖 查看详细优化方案:')
    console.log('   - PERFORMANCE_ANALYSIS.md  (完整分析报告)')
    console.log('   - QUICK_FIX.md             (快速优化方案)')
    console.log('\n')
  } catch (error) {
    console.error('❌ 分析失败:', error.message)
    process.exit(1)
  }
}

main()
