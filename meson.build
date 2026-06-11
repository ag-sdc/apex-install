project('apex-install', version: '1.0.0')

go = find_program('go')

exe = custom_target('apex-install',
  output: 'apex-install',
  command: [go, 'build', '-o', '@OUTPUT@', meson.project_source_root() + '/src/main.go'],
  build_by_default: true,
  install: true,
  install_dir: get_option('bindir')
)

install_data('repo.conf.sample',
  install_dir: '/etc/apex',
  rename: 'repo.conf'
)
