import childProcess from 'node:child_process';
import {syncBuiltinESMExports} from 'node:module';
const original = childProcess.execFileSync;
const inventoryArgs = ['ls-files', '--cached', '--others', '--exclude-standard', '-z'];
childProcess.execFileSync = function (file, args, options) {
  if (file === 'git' && JSON.stringify(args) === JSON.stringify(inventoryArgs)) {
    return original.call(this, file, args, {...options, maxBuffer: 16 * 1024 * 1024});
  }
  return original.call(this, file, args, options);
};
syncBuiltinESMExports();
