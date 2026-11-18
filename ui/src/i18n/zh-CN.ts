// Chinese dictionary. The only file in the UI that holds Chinese text.
export const zh = {
  'app.name': 'Git Glance',
  'app.loading': '加载中…',
  'app.retry': '重试',
  'app.cancel': '取消',
  'app.confirm': '确定',
  'app.close': '关闭',

  'error.noToken': '请打开终端里打印出来的链接',
  'error.generic': '出错了：{message}',

  'chip.unpushed': '没推送过',
  'chip.behind': '落后线上 {n}',
  'chip.ahead': '领先线上 {n}',
  'chip.synced': '和线上一致',
  'chip.dirty': '{n} 处改动',
  'chip.testAll': '已上测试',
  'chip.testSome': '部分上测试 {m}/{n}',
  'chip.testNone': '没上测试',
  'chip.releaseAll': '已上线',
  'chip.releaseSome': '部分上线 {m}/{n}',
  'chip.releaseNone': '未上线',
  'chip.removable': '可以删除文件夹',
  'chip.folderMissing': '文件夹已不存在',

  'block.notInFolder': '这个分支没有自己的文件夹',
  'block.mainFolder': '这是主文件夹，不能删除',
  'block.folderMissing': '文件夹已经不存在',
  'block.dirty': '文件夹里有没提交的改动',
  'block.unpushed': '有还没推送的提交',
  'block.notMerged': '还没有合入主干',
} as const;
