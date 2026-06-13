project('apex-install', version: '1.0.0')

go = find_program('go')

exe = custom_target('apex-install',
  input: ['src/main.go', 'src/config.go', 'src/types.go', 'src/network.go', 'src/database.go', 'src/resolve.go', 'src/install.go', 'src/skip_abi.go'],
  output: 'apex-install',
  command: [go, 'build', '-o', '@OUTPUT@', '@INPUT@'],
  build_by_default: true,
  install: true,
  install_dir: get_option('bindir')
)

install_data('repo.conf.sample',
  install_dir: '/etc/apex',
  rename: 'repo.conf'
)
