Feature: Расследование инцидента кластера
  Команда агентов должна определить первичную область отказа по журналам
  и предложить команду-владельца для исправления.

  Scenario: Ошибка диска etcd приводит к ошибкам DNS
    Given журналы содержат timeout apiserver к etcd
    And журналы содержат fsync latency etcd
    And журналы содержат I/O error на диске etcd
    And журналы содержат timeout CoreDNS к API Kubernetes
    When команда агентов расследует инцидент
    Then основной владелец должен быть storage
    And гипотеза DNS должна быть ослаблена
    And гипотеза etcd должна быть поддержана

  Scenario: Ошибка CNI приводит к отказу сервисной связности
    Given журналы содержат ошибки CNI
    And проверки содержат отказ pod-to-pod связности
    When команда агентов расследует инцидент
    Then основной владелец должен быть network

  Scenario: Среда запуска контейнеров не работает
    Given журналы содержат container runtime is down
    And kubelet сообщает failed to create pod sandbox
    When команда агентов расследует инцидент
    Then основной владелец должен быть runtime

  Scenario: Узел не готов из-за давления по памяти
    Given журналы содержат NodeNotReady
    And журналы содержат MemoryPressure
    When команда агентов расследует инцидент
    Then основной владелец должен быть compute
