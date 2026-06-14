project('apex-install', version: '1.0.0', default_options: ['prefix=/usr', 'sysconfdir=/etc'])

go = find_program('go')

sysconfdir = get_option('sysconfdir')
if not sysconfdir.startswith('/')
  sysconfdir = get_option('prefix') / sysconfdir
endif

exe = custom_target('apex-install',
  input: ['src/main.go', 'src/config.go', 'src/types.go', 'src/network.go', 'src/database.go', 'src/resolve.go', 'src/install.go', 'src/skip_abi.go'],
  output: 'apex-install',
  command: [go, 'build', '-ldflags', '-X main.Sysconfdir=' + sysconfdir, '-o', '@OUTPUT@', '@INPUT@'],
  build_by_default: true,
  install: true,
  install_dir: get_option('bindir')
)

install_data('repo.conf.sample',
  install_dir: sysconfdir / 'apex',
  rename: 'repo.conf'
)
